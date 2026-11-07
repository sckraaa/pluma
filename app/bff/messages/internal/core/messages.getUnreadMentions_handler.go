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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesGetUnreadMentions
// messages.getUnreadMentions#46578472 peer:InputPeer offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (c *MessagesCore) MessagesGetUnreadMentions(in *mtproto.TLMessagesGetUnreadMentions) (*mtproto.Messages_Messages, error) {
	// TODO(@benqi): 重复FromInputPeer2
	var (
		err  error
		peer = mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
		chat *mtproto.MutableChat
		// channel   *channelpb.MutableChannel
		// isChannel bool
		minId   = in.MinId
		limit   = in.Limit
		rValues *mtproto.Messages_Messages
	)

	if limit > 50 {
		limit = 50
	}

	if c.MD.IsBot {
		err = mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getHistory - error: %v", err)
		return nil, err
	}

	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		// 400	CHAT_ID_INVALID	The provided chat id is invalid
		chat, err = c.svcCtx.Dao.ChatClient.Client().ChatGetMutableChat(
			c.ctx,
			&chatpb.TLChatGetMutableChat{
				ChatId: peer.PeerId,
			})
		if err != nil {
			err = mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("messages.getHistory - error: %v", err)
			return nil, err
		} else {
			// TODO(@benqi): check migratedToId
			_ = chat
		}

		rValues = mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
			Messages: []*mtproto.Message{},
			Chats:    []*mtproto.Chat{},
			Users:    []*mtproto.User{},
		}).To_Messages_Messages()
	case mtproto.PEER_CHANNEL:
		// A channel's unread mentions (task 043, biz channel.getUnreadMentions).
		r, err := c.svcCtx.Dao.Channel.ChannelGetUnreadMentions(c.ctx, &channelpb.TLChannelGetUnreadMentions{
			SelfId:    c.MD.UserId,
			ChannelId: peer.PeerId,
			OffsetId:  in.OffsetId,
			AddOffset: in.AddOffset,
			Limit:     limit,
			MaxId:     in.MaxId,
			MinId:     in.MinId,
		})
		if err != nil {
			c.Logger.Errorf("messages.getUnreadMentions - error: %v", err)
			return nil, err
		}
		if err = c.fillChannelPeers(r, peer.PeerId); err != nil {
			c.Logger.Errorf("messages.getUnreadMentions - error: %v", err)
			return nil, err
		}
		return r, nil
	default:
		err = mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("messages.getHistory - error: %v", err)
		return nil, err
	}

	// loadType := calcLoadHistoryType(isChannel, request.OffsetId, request.OffsetDate, request.AddOffset, limit, request.MaxId, request.MinId)
	boxList, _ := c.svcCtx.Dao.MessageClient.MessageGetUnreadMentions(
		c.ctx,
		&messagepb.TLMessageGetUnreadMentions{
			UserId:    c.MD.UserId,
			PeerType:  peer.PeerType,
			PeerId:    peer.PeerId,
			OffsetId:  in.OffsetId,
			AddOffset: in.AddOffset,
			Limit:     limit,
			MinId:     minId,
			MaxInt:    in.MaxId,
		})

	//if isChannel {
	//	rValues.Count = boxList.Length()
	//}

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
			//mChannels, _ := c.svcCtx.Dao.ChannelClient.ChannelGetChannelListByIdList(c.ctx,
			//	&channelpb.TLChannelGetChannelListByIdList{
			//		SelfUserId: c.MD.UserId,
			//		Id:         channelIdList,
			//	})
			//if len(mChannels.GetDatas()) > 0 {
			//	rValues.Chats = append(rValues.Chats, mChannels.GetDatas()...)
			//}
		})

	return rValues, nil
}
