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
	"math"
	"slices"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/msgsearch"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesSearch
// messages.search#a0fda762 flags:# peer:InputPeer q:string from_id:flags.0?InputPeer top_msg_id:flags.1?int filter:MessagesFilter min_date:int max_date:int offset_id:int add_offset:int limit:int max_id:int min_id:int hash:long = messages.Messages;
func (c *MessagesCore) MessagesSearch(in *mtproto.TLMessagesSearch) (*mtproto.Messages_Messages, error) {
	// 400	BOT_METHOD_INVALID	This method can't be used by a bot
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}

	limit := min(max(in.Limit, 0), msgsearch.MaxLimit)
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	if peer.IsChannel() {
		return c.searchChannel(in, peer.PeerId, limit)
	}

	kind, ok := msgsearch.KindOf(in.Filter)
	if !ok {
		err := mtproto.ErrInputFilterInvalid
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}

	var (
		fromId int64
		err    error
	)
	if in.GetFromId() != nil {
		from := mtproto.FromInputPeer2(c.MD.UserId, in.FromId)
		switch from.PeerType {
		case mtproto.PEER_USER, mtproto.PEER_SELF:
			fromId = from.PeerId
		case mtproto.PEER_CHANNEL:
			// A channel never writes into a box; nothing can match.
			return emptyMessages(), nil
		default:
			err = mtproto.ErrFromPeerInvalid
			c.Logger.Errorf("messages.search - error: %v", err)
			return nil, err
		}
	}

	if len(in.SavedReaction) > 0 {
		// The tags of Saved Messages (task 053).
		offsetId := in.OffsetId
		if offsetId == 0 {
			offsetId = math.MaxInt32
		}
		boxList, err := c.searchTagged(in, peer, offsetId, limit)
		if err != nil {
			c.Logger.Errorf("messages.search - error: %v", err)
			return nil, err
		}
		return c.searchResult(emptyMessages(), boxList), nil
	}

	if kind == msgsearch.KindEmpty && in.Q == "" && fromId == 0 && in.SavedPeerId == nil {
		err = mtproto.ErrSearchQueryEmpty
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	if in.GetTopMsgId() != nil {
		// Only a channel has threads: a user's box has none to look in.
		return emptyMessages(), nil
	}

	q := msgsearch.BoxQuery{
		UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId,
		Q: in.Q, Kind: kind, FromId: fromId, MinDate: in.MinDate, MaxDate: in.MaxDate,
	}
	if in.SavedPeerId != nil {
		saved := mtproto.FromInputPeer2(c.MD.UserId, in.SavedPeerId)
		if saved.PeerType == mtproto.PEER_SELF {
			saved = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
		}
		q.HasSaved, q.SavedPeerType, q.SavedPeerId = true, saved.PeerType, saved.PeerId
	}
	ids, count, err := msgsearch.SearchBox(c.ctx, c.svcCtx.Search, q, msgsearch.Page{
		OffsetId: in.OffsetId, AddOffset: in.AddOffset, Limit: limit, MaxId: in.MaxId, MinId: in.MinId,
	})
	if err != nil {
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	r, err := c.boxPage(ids, count)
	if err != nil {
		c.Logger.Errorf("messages.search - error: %v", err)
		return nil, err
	}
	return r, c.fillMessagesPeers(r)
}

func emptyMessages() *mtproto.Messages_Messages {
	return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
		Messages: []*mtproto.Message{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}).To_Messages_Messages()
}

// boxPage is the box messages ids of the account as messages, newest
// first, messagesSlice with the count when they are not all that matched.
func (c *MessagesCore) boxPage(ids []int32, count int32) (*mtproto.Messages_Messages, error) {
	r := emptyMessages()
	if int(count) > len(ids) {
		r.PredicateName = mtproto.Predicate_messages_messagesSlice
		r.Count = count
	}
	if len(ids) == 0 {
		return r, nil
	}
	boxes, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: ids})
	if err != nil {
		return nil, err
	}
	list := mtproto.MakeTLMessageBoxList(&mtproto.MessageBoxList{BoxList: boxes.GetDatas()}).To_MessageBoxList()
	list.Visit(c.MD.UserId,
		func(messages []*mtproto.Message) { r.Messages = messages },
		func([]int64) {}, func([]int64) {}, func([]int64) {})
	slices.SortFunc(r.Messages, func(a, b *mtproto.Message) int { return int(b.GetId()) - int(a.GetId()) })
	return r, nil
}

// fillMessagesPeers adds to r the users and chats of its messages.
func (c *MessagesCore) fillMessagesPeers(r *mtproto.Messages_Messages) error {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(r.GetMessages()...)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	r.Chats = []*mtproto.Chat{}
	if len(h.ChatIdList) > 0 {
		chats, err := c.svcCtx.Dao.ChatClient.Client().ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{IdList: h.ChatIdList})
		if err != nil {
			return err
		}
		r.Chats = chats.GetChatListByIdList(c.MD.UserId, h.ChatIdList...)
	}
	if len(h.ChannelIdList) > 0 {
		channels, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: h.ChannelIdList})
		if err != nil {
			return err
		}
		r.Chats = append(r.Chats, channels.GetChannelListByIdList(c.MD.UserId)...)
	}
	return nil
}

// searchResult fills rValues with the messages of boxList as the account sees
// them, and their users and chats.
func (c *MessagesCore) searchResult(rValues *mtproto.Messages_Messages, boxList *mtproto.MessageBoxList) *mtproto.Messages_Messages {
	boxList.Visit(c.MD.UserId,
		func(messageList []*mtproto.Message) {
			rValues.Messages = messageList
		},
		func(userIdList []int64) {
			mUsers, _ := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx,
				&userpb.TLUserGetMutableUsers{
					Id: userIdList,
				})
			rValues.Users = append(rValues.Users, mUsers.GetUserListByIdList(c.MD.UserId, userIdList...)...)
		},
		func(chatIdList []int64) {
			mChats, _ := c.svcCtx.Dao.ChatClient.Client().ChatGetChatListByIdList(c.ctx,
				&chatpb.TLChatGetChatListByIdList{
					IdList: chatIdList,
				})
			rValues.Chats = append(rValues.Chats, mChats.GetChatListByIdList(c.MD.UserId, chatIdList...)...)
		},
		func(channelIdList []int64) {
			// ignore it
		})

	return rValues
}

// searchTagged is the search of Saved Messages by tags: the account's
// messages there with all the tags of in.SavedReaction, of one saved dialog
// with in.SavedPeerId when given, newest first below offsetId, with the
// text q when given (task 053).
func (c *MessagesCore) searchTagged(in *mtproto.TLMessagesSearch, peer *mtproto.PeerUtil, offsetId, limit int32) (*mtproto.MessageBoxList, error) {
	empty := mtproto.MakeTLMessageBoxList(&mtproto.MessageBoxList{BoxList: []*mtproto.MessageBox{}}).To_MessageBoxList()
	if peer.PeerType != mtproto.PEER_SELF && (peer.PeerType != mtproto.PEER_USER || peer.PeerId != c.MD.UserId) {
		return empty, nil
	}
	keys, err := reactions.Keys(in.SavedReaction)
	if err != nil {
		return nil, err
	}
	var savedPeer *mtproto.PeerUtil
	if in.SavedPeerId != nil {
		savedPeer = mtproto.FromInputPeer2(c.MD.UserId, in.SavedPeerId)
		if savedPeer.PeerType == mtproto.PEER_SELF {
			savedPeer = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
		}
	}
	ids, err := reactions.Tagged(c.ctx, c.svcCtx.Reactions, c.MD.UserId, keys, savedPeer, in.Q, offsetId, min(max(limit, 1), 100))
	if err != nil || len(ids) == 0 {
		return empty, err
	}
	boxes, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: ids})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessageBoxList(&mtproto.MessageBoxList{BoxList: boxes.GetDatas()}).To_MessageBoxList(), nil
}
