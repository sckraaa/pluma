// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core

import (
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/msgsearch"
)

// Archive folder of dialogs and the folder modes of channel.searchAcross.
const (
	archiveFolderId = 1
	folderKeep      = 1
	folderDrop      = 2
)

// searchGlobal answers messages.searchGlobal: the matching messages of the
// account's private chats and basic groups (its box) and of its channels,
// merged newest first, as Android reads them (DialogsSearchAdapter:
// next_rate, then offset_rate/offset_peer/offset_id of the last message).
// folder_id 0 means every chat out of the archive, 1 the archive
// (tdesktop dialogs_widget.cpp "restrictFolder",
// data_search_controller.cpp f_folder_id "No archive"); no folder_id means
// all of them.
func (c *MessagesCore) searchGlobal(in *mtproto.TLMessagesSearchGlobal) (*mtproto.Messages_Messages, error) {
	kind, ok := msgsearch.KindOf(in.Filter)
	if !ok {
		return nil, mtproto.ErrInputFilterInvalid
	}
	if kind == msgsearch.KindEmpty && in.Q == "" {
		return nil, mtproto.ErrSearchQueryEmpty
	}
	limit := min(max(in.Limit, 1), msgsearch.MaxLimit)

	var (
		folderMode int32
		archived   msgsearch.PeerSet
	)
	if in.FolderId != nil {
		switch in.FolderId.GetValue() {
		case 0:
			folderMode = folderDrop
		case archiveFolderId:
			folderMode = folderKeep
		default:
			return nil, mtproto.ErrFolderIdInvalid
		}
		var err error
		if archived, err = msgsearch.ArchivedPeers(c.ctx, c.svcCtx.Search, c.MD.UserId); err != nil {
			return nil, err
		}
	}
	communityId := in.GetCommunity().GetChannelId()
	if in.GetCommunity() != nil {
		// A community is one supergroup-like channel: its own messages.
		folderMode = 0
	}

	offsetPeer := mtproto.FromInputPeer2(c.MD.UserId, in.GetOffsetPeer())
	var (
		box      *mtproto.Messages_Messages
		boxCount int32
		boxMore  bool
	)
	if !in.BroadcastsOnly && in.GetCommunity() == nil {
		q := msgsearch.BoxQuery{
			UserId: c.MD.UserId, PeerType: mtproto.PEER_EMPTY,
			Q: in.Q, Kind: kind, MinDate: in.MinDate, MaxDate: in.MaxDate,
		}
		if in.UsersOnly {
			q.Extra.And("peer_type = ?", mtproto.PEER_USER)
		}
		if in.GroupsOnly {
			q.Extra.And("peer_type = ?", mtproto.PEER_CHAT)
		}
		if folderMode != 0 {
			archived.NarrowBox(&q.Extra, folderMode == folderKeep)
		}
		if in.OffsetRate > 0 {
			if offsetPeer.IsChannel() {
				q.Cursor.And("date2 < ?", in.OffsetRate)
			} else if in.OffsetId > 0 {
				q.Cursor.And("user_message_box_id < ?", in.OffsetId)
			} else {
				q.Cursor.And("date2 <= ?", in.OffsetRate)
			}
		}
		ids, count, err := msgsearch.SearchBox(c.ctx, c.svcCtx.Search, q, msgsearch.Page{Limit: limit})
		if err != nil {
			return nil, err
		}
		boxCount = count
		boxMore = int32(len(ids)) == limit
		if box, err = c.boxPage(ids, int32(len(ids))); err != nil {
			return nil, err
		}
	}

	var (
		channels     []*mtproto.Message
		channelCount int32
		channelMore  bool
	)
	if !in.UsersOnly {
		offsetChannel := int64(0)
		if offsetPeer.IsChannel() {
			offsetChannel = offsetPeer.PeerId
		}
		r, err := c.svcCtx.Dao.Channel.ChannelSearchAcross(c.ctx, &channelpb.TLChannelSearchAcross{
			SelfId:          c.MD.UserId,
			PublicOnly:      mtproto.BoolFalse,
			BroadcastsOnly:  mtproto.ToBool(in.BroadcastsOnly),
			GroupsOnly:      mtproto.ToBool(in.GroupsOnly),
			CommunityId:     communityId,
			FolderMode:      folderMode,
			FolderChannels:  archived.Channels,
			Q:               in.Q,
			MediaType:       kind,
			MinDate:         in.MinDate,
			MaxDate:         in.MaxDate,
			OffsetRate:      in.OffsetRate,
			OffsetChannelId: offsetChannel,
			OffsetId:        in.OffsetId,
			Limit:           limit,
		})
		if err != nil {
			return nil, err
		}
		channels, channelCount = r.GetMessages(), r.GetCount()
		channelMore = r.GetNextRate() != nil
	}

	return c.mergedPage(box.GetMessages(), channels, boxCount+channelCount, limit, boxMore || channelMore)
}

// mergedPage joins the two sources newest first, a box message before a
// channel's of the same second, cut to limit.
func (c *MessagesCore) mergedPage(box, channels []*mtproto.Message, count, limit int32, sourceMore bool) (*mtproto.Messages_Messages, error) {
	merged := make([]*mtproto.Message, 0, len(box)+len(channels))
	merged = append(append(merged, box...), channels...)
	isChannel := func(m *mtproto.Message) bool {
		return m.GetPeerId().GetPredicateName() == mtproto.Predicate_peerChannel
	}
	slices.SortStableFunc(merged, func(a, b *mtproto.Message) int {
		if d := int(b.GetDate()) - int(a.GetDate()); d != 0 {
			return d
		}
		if ca, cb := isChannel(a), isChannel(b); ca != cb {
			if ca {
				return 1
			}
			return -1
		}
		return 0
	})
	more := sourceMore
	if int32(len(merged)) > limit {
		merged = merged[:limit]
		more = true
	}
	r := emptyMessages()
	r.Messages = merged
	if more || int32(len(merged)) < count {
		r.PredicateName = mtproto.Predicate_messages_messagesSlice
		r.Count = count
		if more && len(merged) > 0 {
			r.NextRate = mtproto.MakeFlagsInt32(merged[len(merged)-1].GetDate())
		}
	}
	return r, c.fillMessagesPeers(r)
}

// searchSentMediaKinds are the media of the account's messages that
// messages.searchSentMedia gives for the empty filter.
var searchSentMediaKinds = []int32{
	mtproto.MEDIA_PHOTOS_ONLY, mtproto.MEDIA_VIDEOS_ONLY, mtproto.MEDIA_FILE,
	mtproto.MEDIA_MUSIC, mtproto.MEDIA_GIF, mtproto.MEDIA_AUDIO,
}

// searchSentMedia answers messages.searchSentMedia: the account's own
// sent media, newest first, no paging (iOS AttachmentFileController loads
// recent documents with it). Filters that are no kind of media are
// FILTER_NOT_SUPPORTED (core.telegram.org/method/messages.searchSentMedia).
func (c *MessagesCore) searchSentMedia(in *mtproto.TLMessagesSearchSentMedia) (*mtproto.Messages_Messages, error) {
	kind, ok := msgsearch.KindOf(in.Filter)
	if !ok {
		return nil, mtproto.ErrFilterNotSupported
	}
	q := msgsearch.BoxQuery{UserId: c.MD.UserId, PeerType: mtproto.PEER_EMPTY, Q: in.Q, FromId: c.MD.UserId}
	if kind == msgsearch.KindEmpty {
		q.Kind = msgsearch.KindEmpty
		marks, args := "?", []any{searchSentMediaKinds[0]}
		for _, k := range searchSentMediaKinds[1:] {
			marks += ", ?"
			args = append(args, k)
		}
		q.Extra.And("message_filter_type IN ("+marks+")", args...)
	} else if isSentMediaKind(kind) {
		q.Kind = kind
	} else {
		return nil, mtproto.ErrFilterNotSupported
	}
	ids, _, err := msgsearch.SearchBox(c.ctx, c.svcCtx.Search, q, msgsearch.Page{Limit: min(max(in.Limit, 1), msgsearch.MaxLimit)})
	if err != nil {
		return nil, err
	}
	r, err := c.boxPage(ids, int32(len(ids)))
	if err != nil {
		return nil, err
	}
	return r, c.fillMessagesPeers(r)
}

func isSentMediaKind(kind int32) bool {
	return slices.Contains(searchSentMediaKinds, kind) || kind == mtproto.MEDIA_PHOTOVIDEO ||
		kind == mtproto.MEDIA_VOICE_FILE || kind == mtproto.MEDIA_ROUND_FILE
}

// searchPosts answers channels.searchPosts over all public channels: a
// hashtag or a text query, not both. Text queries are counted against the
// daily quota, pagination and repeated queries free of it
// (searchPostsFlood). The empty query is what the clients open the list
// with (tdesktop PostsSearch::applyQuery, Android PostsSearchContainer
// "news"): it gives the newest posts and takes no slot. Which posts
// Telegram puts there is not documented. Stars are not taken here, so
// allow_paid_stars is never needed; stars_amount says 0.
func (c *MessagesCore) searchPosts(in *mtproto.TLChannelsSearchPosts) (*mtproto.Messages_Messages, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	hashtag := in.GetHashtag_FLAGSTRING().GetValue()
	if hashtag == "" {
		hashtag = in.GetHashtag_STRING()
	}
	query := in.GetQuery().GetValue()
	if (hashtag != "" && query != "") || (hashtag == "" && in.GetQuery() == nil) {
		return nil, mtproto.ErrSearchQueryEmpty
	}
	offsetPeer := mtproto.FromInputPeer2(c.MD.UserId, in.GetOffsetPeer())
	if offsetPeer.PeerType != mtproto.PEER_EMPTY && !offsetPeer.IsChannel() {
		return nil, mtproto.ErrOffsetPeerIdInvalid
	}
	var flood *mtproto.SearchPostsFlood
	if query != "" {
		var err error
		if in.OffsetRate == 0 && in.OffsetId == 0 {
			flood, err = msgsearch.TakePostsSlot(c.ctx, c.svcCtx.Search, c.MD.UserId, query, time.Now())
		} else {
			flood, err = msgsearch.PostsFlood(c.ctx, c.svcCtx.Search, c.MD.UserId, query, time.Now())
		}
		if err != nil {
			return nil, err
		}
	}
	text := query
	if hashtag != "" {
		text = msgsearch.PostsHashtag(hashtag)
	}
	offsetChannel := int64(0)
	if offsetPeer.IsChannel() {
		offsetChannel = offsetPeer.PeerId
	}
	r, err := c.svcCtx.Dao.Channel.ChannelSearchAcross(c.ctx, &channelpb.TLChannelSearchAcross{
		SelfId:          c.MD.UserId,
		PublicOnly:      mtproto.BoolTrue,
		BroadcastsOnly:  mtproto.BoolTrue,
		GroupsOnly:      mtproto.BoolFalse,
		Q:               text,
		MediaType:       msgsearch.KindEmpty,
		OffsetRate:      in.OffsetRate,
		OffsetChannelId: offsetChannel,
		OffsetId:        in.OffsetId,
		Limit:           min(max(in.Limit, 1), msgsearch.MaxLimit),
	})
	if err != nil {
		return nil, err
	}
	r.SearchFlood = flood
	if err = c.fillMessagesPeers(r); err != nil {
		return nil, err
	}
	return r, nil
}
