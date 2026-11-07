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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesGetMessagesReactions
// messages.getMessagesReactions#8bba90e6 peer:InputPeer id:Vector<int> = Updates;
//
// The reactions of the account's messages id of the dialog, an
// updateMessageReactions for each one that exists, an empty object for one
// without reactions: Desktop clears the reactions of a polled message
// nothing came for (data_message_reactions.cpp:1731-1736, task 053).
func (c *CatalogCore) MessagesGetMessagesReactions(in *mtproto.TLMessagesGetMessagesReactions) (*mtproto.Updates, error) {
	if p := mtproto.FromInputPeer2(c.MD.UserId, in.Peer); p.PeerType == mtproto.PEER_CHANNEL {
		return c.channelMessagesReactions(p.PeerId, in.Id)
	}
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	if len(in.Id) > 100 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	updates := []*mtproto.Update{}
	if len(in.Id) > 0 {
		boxes, err := c.svcCtx.Message.MessageGetUserMessageList(c.ctx, &messagepb.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: in.Id})
		if err != nil {
			c.Logger.Errorf("messages.getMessagesReactions - error: %v", err)
			return nil, err
		}
		for _, box := range boxes.GetDatas() {
			if box.GetPeerType() != peer.PeerType || box.GetPeerId() != peer.PeerId || box.GetMessage() == nil {
				continue
			}
			view := box.GetMessage().GetReactions()
			if view == nil {
				view = reactions.ViewOrEmpty(nil, c.MD.UserId, box.GetSenderUserId(), reactions.BoxKind(box))
			}
			updates = append(updates, mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
				Peer_PEER:                  peer.ToPeer(),
				MsgId_INT32:                box.GetMessageId(),
				Reactions_MESSAGEREACTIONS: view,
			}).To_Update())
		}
	}
	users, chats, err := c.peersOf(peer, reactorsOf(updates...))
	if err != nil {
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdatesUsersChats(users, chats, updates...), nil
}

// channelMessagesReactions is messages.getMessagesReactions in a channel
// (task 044): the messages as the account sees them carry their reactions
// (biz channel.getMessages), one update each, an empty object for one
// without; ids the account may not see are left out.
func (c *CatalogCore) channelMessagesReactions(channelId int64, ids []int32) (*mtproto.Updates, error) {
	if len(ids) > 100 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	updates := []*mtproto.Update{}
	if len(ids) > 0 {
		messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: channelId, Id: ids})
		if err != nil {
			c.Logger.Errorf("messages.getMessagesReactions - error: %v", err)
			return nil, err
		}
		kind := reactions.ChannelKind(mc.GetChannel().GetBroadcast())
		for _, m := range messages.GetDatas() {
			if m.GetPredicateName() != mtproto.Predicate_message {
				continue
			}
			view := m.GetReactions()
			if view == nil {
				view = reactions.ViewOrEmpty(nil, c.MD.UserId, m.GetFromId().GetUserId(), kind)
			}
			updates = append(updates, mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
				Peer_PEER:                  mtproto.MakePeerChannel(channelId),
				MsgId_INT32:                m.GetId(),
				Reactions_MESSAGEREACTIONS: view,
			}).To_Update())
		}
	}
	users, _, err := c.peersOf(mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, channelId), reactorsOf(updates...))
	if err != nil {
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdatesUsersChats(users, []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))}, updates...), nil
}
