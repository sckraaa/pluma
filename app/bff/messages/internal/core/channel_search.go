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
	"github.com/teamgram/teamgram-server/pkg/msgsearch"
)

// searchChannel is messages.search in a channel or a supergroup (task 043,
// biz channel.search): text, author, media lists and the pinned messages,
// which the clients load for the pinned bar. The answer is
// messages.channelMessages with the count of all that match. A text search
// needs q or from_id (SEARCH_QUERY_EMPTY). A limit of 0 answers with the
// count alone (core.telegram.org/method/messages.search).
func (c *MessagesCore) searchChannel(in *mtproto.TLMessagesSearch, channelId int64, limit int32) (*mtproto.Messages_Messages, error) {
	mediaType, ok := msgsearch.KindOf(in.Filter)
	if !ok {
		return nil, mtproto.ErrInputFilterInvalid
	}
	var fromId int64
	if in.FromId != nil {
		from := mtproto.FromInputPeer2(c.MD.UserId, in.FromId)
		switch from.PeerType {
		case mtproto.PEER_USER, mtproto.PEER_SELF:
			fromId = from.PeerId
		case mtproto.PEER_CHANNEL:
			// channel.search takes a channel as the sender by -id.
			fromId = -from.PeerId
		default:
			return nil, mtproto.ErrFromPeerInvalid
		}
	}
	if mediaType == msgsearch.KindEmpty && in.Q == "" && fromId == 0 {
		return nil, mtproto.ErrSearchQueryEmpty
	}
	if in.SavedPeerId != nil || len(in.SavedReaction) > 0 {
		return emptyMessages(), nil
	}
	offsetId := in.OffsetId
	var (
		r   *mtproto.Messages_Messages
		err error
	)
	pageLimit := max(limit, 1)
	if top := in.GetTopMsgId(); top != nil {
		// A search in a topic, or in a thread (task 060).
		r, err = c.svcCtx.Dao.Channel.ChannelSearchTopic(c.ctx, &channelpb.TLChannelSearchTopic{
			SelfId:         c.MD.UserId,
			ChannelId:      channelId,
			TopMsgId:       top.GetValue(),
			Q:              in.Q,
			FromId:         fromId,
			MediaType:      mediaType,
			UnreadMentions: mtproto.BoolFalse,
			MinDate:        in.MinDate,
			MaxDate:        in.MaxDate,
			OffsetId:       offsetId,
			AddOffset:      in.AddOffset,
			Limit:          pageLimit,
			MaxId:          in.MaxId,
			MinId:          in.MinId,
		})
		if err != nil {
			c.Logger.Errorf("messages.search - error: %v", err)
			return nil, err
		}
		return c.channelSearchResult(r, channelId, limit)
	}
	r, err = c.svcCtx.Dao.Channel.ChannelSearch(c.ctx, &channelpb.TLChannelSearch{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
		Q:         in.Q,
		FromId:    fromId,
		MediaType: mediaType,
		MinDate:   in.MinDate,
		MaxDate:   in.MaxDate,
		OffsetId:  offsetId,
		AddOffset: in.AddOffset,
		Limit:     pageLimit,
		MaxId:     in.MaxId,
		MinId:     in.MinId,
	})
	if err != nil {
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	return c.channelSearchResult(r, channelId, limit)
}

func (c *MessagesCore) channelSearchResult(r *mtproto.Messages_Messages, channelId int64, limit int32) (*mtproto.Messages_Messages, error) {
	if limit == 0 {
		r.Messages = []*mtproto.Message{}
	}
	if err := c.fillChannelPeers(r, channelId); err != nil {
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	return r, nil
}
