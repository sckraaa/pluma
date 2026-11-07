// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// channelSearchType is the kind of message biz channel.search looks for by
// a filter of messages.search (the media types the messages are stored
// with, mtproto.GetMediaType): MEDIA_EMPTY for any. Voices and round
// videos are one stored type, MEDIA_AUDIO, as in the user's boxes. ok is
// false for a filter a channel has nothing for (calls, mentions, places,
// contacts) or that cannot be told apart (round videos alone).
func channelSearchType(f mtproto.MessagesFilterType) (t int32, ok bool) {
	switch f {
	case mtproto.FilterEmpty:
		return mtproto.MEDIA_EMPTY, true
	case mtproto.FilterPhotos:
		return mtproto.MEDIA_PHOTOS_ONLY, true
	case mtproto.FilterVideo:
		return mtproto.MEDIA_VIDEOS_ONLY, true
	case mtproto.FilterPhotoVideo:
		return mtproto.MEDIA_PHOTOVIDEO, true
	case mtproto.FilterDocument:
		return mtproto.MEDIA_FILE, true
	case mtproto.FilterUrl:
		return mtproto.MEDIA_URL, true
	case mtproto.FilterGif:
		return mtproto.MEDIA_GIF, true
	case mtproto.FilterMusic:
		return mtproto.MEDIA_MUSIC, true
	case mtproto.FilterChatPhotos:
		return mtproto.MEDIA_CHAT_PHOTO, true
	case mtproto.FilterVoice, mtproto.FilterRoundVoice:
		return mtproto.MEDIA_AUDIO, true
	case mtproto.FilterPinned:
		return mtproto.MEDIA_PINNED, true
	}
	return 0, false
}

// searchChannel is messages.search in a channel or a supergroup (task 043,
// biz channel.search): text, author, media lists and the pinned messages,
// which the clients load for the pinned bar. The answer is
// messages.channelMessages with the count of all that match. A text search
// needs q or from_id (SEARCH_QUERY_EMPTY).
func (c *MessagesCore) searchChannel(in *mtproto.TLMessagesSearch, channelId int64, offsetId, limit int32) (*mtproto.Messages_Messages, error) {
	filter := mtproto.FromMessagesFilter(in.Filter)
	mediaType, ok := channelSearchType(filter)
	if !ok {
		return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
			Messages: []*mtproto.Message{}, Chats: []*mtproto.Chat{}, Users: []*mtproto.User{},
		}).To_Messages_Messages(), nil
	}
	var fromId int64
	if in.FromId != nil {
		from := mtproto.FromInputPeer2(c.MD.UserId, in.FromId)
		if from.PeerType != mtproto.PEER_USER && from.PeerType != mtproto.PEER_SELF {
			return nil, mtproto.ErrFromPeerInvalid
		}
		fromId = from.PeerId
	}
	if filter == mtproto.FilterEmpty && in.Q == "" && fromId == 0 {
		return nil, mtproto.ErrSearchQueryEmpty
	}
	r, err := c.svcCtx.Dao.Channel.ChannelSearch(c.ctx, &channelpb.TLChannelSearch{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
		Q:         in.Q,
		FromId:    fromId,
		MediaType: mediaType,
		MinDate:   in.MinDate,
		MaxDate:   in.MaxDate,
		OffsetId:  offsetId,
		AddOffset: in.AddOffset,
		Limit:     limit,
		MaxId:     in.MaxId,
		MinId:     in.MinId,
	})
	if err != nil {
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	if err = c.fillChannelPeers(r, channelId); err != nil {
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	return r, nil
}
