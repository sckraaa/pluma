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
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// MessagesSetTyping
// messages.setTyping#58943ee2 flags:# peer:InputPeer top_msg_id:flags.0?int action:SendMessageAction = Bool;
func (c *DialogsCore) MessagesSetTyping(in *mtproto.TLMessagesSetTyping) (*mtproto.Bool, error) {
	var (
		peer = mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
		date = int32(time.Now().Unix())
	)

	// TODO(@benqi): push chat/channel时机
	switch peer.PeerType {
	case mtproto.PEER_USER:
		updates := mtproto.MakeTLUpdateShort(&mtproto.Updates{
			Update: mtproto.MakeTLUpdateUserTyping(&mtproto.Update{
				UserId: c.MD.UserId,
				Action: in.Action,
			}).To_Update(),
			Date: date,
		}).To_Updates()
		c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &sync.TLSyncPushUpdates{
			UserId:  peer.PeerId,
			Updates: updates,
		})
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{
			ChatId: peer.PeerId,
		})
		_ = err
		updates := mtproto.MakeTLUpdateShort(&mtproto.Updates{
			// updateChatUserTyping#86cadb6c chat_id:int from_id:Peer action:SendMessageAction = Update;
			Update: mtproto.MakeTLUpdateChatUserTyping(&mtproto.Update{
				ChatId_INT64: peer.PeerId,
				FromId:       mtproto.MakePeerUser(c.MD.UserId),
				Action:       in.Action,
			}).To_Update(),
			Date: date,
		}).To_Updates()
		chat.Walk(func(userId int64, participant *mtproto.ImmutableChatParticipant) error {
			if userId == c.MD.UserId {
				return nil
			}
			if participant.IsChatMemberStateNormal() {
				c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &sync.TLSyncPushUpdates{
					UserId:  userId,
					Updates: updates,
				})
			}
			return nil
		})
	case mtproto.PEER_CHANNEL:
		// updateChannelUserTyping to the other members in a supergroup, with
		// the topic or the thread (task 067); a broadcast has no typing. A
		// signal of a few seconds: a failure costs nothing, it is logged.
		if err := c.channelTyping(peer.PeerId, in, date); err != nil {
			c.Logger.Errorf("messages.setTyping - error: %v", err)
		}
	}

	return mtproto.BoolTrue, nil
}

// channelTyping pushes the action of the member to the other members of the
// supergroup. The clients show it in the topic of top_msg_id
// (core.telegram.org/constructor/updateChannelUserTyping, tdesktop
// api_updates.cpp:2020-2027).
func (c *DialogsCore) channelTyping(channelId int64, in *mtproto.TLMessagesSetTyping, date int32) error {
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId})
	if err != nil {
		return err
	}
	if mc.GetChannel().GetBroadcast() {
		return nil
	}
	members, err := c.svcCtx.Dao.Channel.ChannelGetMemberIdList(c.ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: channelId})
	if err != nil {
		return err
	}
	if !slices.Contains(members.GetDatas(), c.MD.UserId) {
		return nil
	}
	to := members.GetDatas()
	if mc.GetChannel().GetMonoforum() {
		if mc, err = c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: to}); err != nil {
			return err
		}
		to = monoforumTypingTo(mc, c.MD.UserId)
	}
	u := mtproto.MakeTLUpdateChannelUserTyping(&mtproto.Update{
		ChannelId: channelId,
		FromId:    mtproto.MakePeerUser(c.MD.UserId),
		Action:    in.Action,
	}).To_Update()
	if top := in.GetTopMsgId(); top != nil {
		u.TopMsgId_FLAGINT32 = top
	}
	updates := mtproto.MakeTLUpdateShort(&mtproto.Updates{Update: u, Date: date}).To_Updates()
	for _, id := range to {
		if id == c.MD.UserId {
			continue
		}
		if _, err = c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &sync.TLSyncPushUpdates{UserId: id, Updates: updates}); err != nil {
			return err
		}
	}
	return nil
}

// monoforumTypingTo are those who see the typing of userId in the direct
// messages of a channel (task 084, ADR 0055), mc being the monoforum with
// the participants of its recipients. A subscriber types to the admins of
// the direct messages alone: to the other subscribers it is in no chat of
// theirs. An admin answers as the channel, and its typing would name it to
// the subscriber; setTyping does not say which sublist it is in either
// (messages.setTyping of layer 228 has no saved_peer_id), so it goes to
// nobody. tdesktop does not send it from a sublist at all
// (history_view_chat_section.cpp:676-679).
func monoforumTypingTo(mc *channelpb.MutableChannel, userId int64) []int64 {
	if me, _ := mc.GetParticipant(userId); channelpb.SublistAdmin(me) {
		return nil
	}
	var to []int64
	for _, p := range mc.GetParticipants() {
		if p.GetUserId() != userId && channelpb.SublistAdmin(p) {
			to = append(to, p.GetUserId())
		}
	}
	return to
}
