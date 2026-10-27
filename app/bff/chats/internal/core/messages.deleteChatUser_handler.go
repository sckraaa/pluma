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
	"math/rand"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// MessagesDeleteChatUser
// messages.deleteChatUser#a2185cab flags:# revoke_history:flags.0?true chat_id:long user_id:InputUser = Updates;
//
// As in Telegram, for every client: biz chat removes the user, or the user
// leaves, and gives the members and the removed user their updates with seq
// (ADR 0021); the service message goes to the members and to the removed
// user, who keeps the dialog: the chat is forbidden (or left) for them, and
// a client deletes it with messages.deleteHistory (Delete and leave). The
// reply is the message's with the members, or for one who left the
// forbidden list.
func (c *ChatsCore) MessagesDeleteChatUser(in *mtproto.TLMessagesDeleteChatUser) (*mtproto.Updates, error) {
	deleteUser := mtproto.FromInputUser(c.MD.UserId, in.UserId)

	if !deleteUser.IsUser() || deleteUser.PeerId == 0 {
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("messages.deleteChatUser - invalid peer: %v", err)
		return nil, err
	}

	operatorId := c.MD.UserId
	if c.MD.IsAdmin {
		operatorId = 0
	}

	chat, err := c.svcCtx.Dao.ChatClient.Client().ChatDeleteChatUser(c.ctx, &chatpb.TLChatDeleteChatUser{
		ChatId:       in.ChatId,
		OperatorId:   operatorId,
		DeleteUserId: deleteUser.PeerId,
	})
	if err != nil {
		c.Logger.Errorf("messages.deleteChatUser - error: %v", err)
		return nil, err
	}

	fromId := c.MD.UserId
	if c.MD.IsAdmin {
		fromId = chat.Creator()
	}

	replyUpdates, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(
		c.ctx,
		&msgpb.TLMsgSendMessageV2{
			UserId:    fromId,
			AuthKeyId: c.MD.PermAuthKeyId,
			PeerType:  mtproto.PEER_CHAT,
			PeerId:    in.ChatId,
			Message: []*msgpb.OutboxMessage{
				msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
					NoWebpage:    true,
					Background:   false,
					RandomId:     rand.Int63(), //nolint:gosec // random_id of a service message, not a secret
					Message:      chat.MakeMessageService(fromId, mtproto.MakeMessageActionChatDeleteUser(deleteUser.PeerId)),
					ScheduleDate: nil,
				}).To_OutboxMessage(),
			},
		})
	if err != nil {
		c.Logger.Errorf("messages.deleteChatUser - error: %v", err)
		return nil, err
	}

	participants := chat.ToChatParticipants(0)
	if me, _ := chat.GetImmutableChatParticipant(fromId); me != nil && !me.IsChatMemberStateNormal() {
		participants = mtproto.MakeTLChatParticipantsForbidden(&mtproto.ChatParticipants{ChatId: in.ChatId}).To_ChatParticipants()
	}
	replyUpdates.Updates = append(replyUpdates.Updates, mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
		Participants_CHATPARTICIPANTS: participants,
	}).To_Update())
	return replyUpdates, nil
}
