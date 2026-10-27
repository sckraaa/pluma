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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// MessagesDeleteChat
// messages.deleteChat#5bd0ee50 chat_id:long = Bool;
//
// biz chat gives every member and this account updateChat with seq in the
// transaction that deletes the chat (ADR 0021).
func (c *ChatsCore) MessagesDeleteChat(in *mtproto.TLMessagesDeleteChat) (*mtproto.Bool, error) {
	operatorId := c.MD.UserId
	if c.MD.IsAdmin {
		operatorId = 0
	}

	// 2. delete chat
	chat, err := c.svcCtx.Dao.ChatClient.Client().ChatDeleteChat(c.ctx, &chatpb.TLChatDeleteChat{
		ChatId:     in.ChatId,
		OperatorId: operatorId,
	})
	if err != nil {
		c.Logger.Errorf("messages.deleteChat - error: %v", err)
		return nil, err
	}

	// 1. kicked all: each member's history of the chat goes, with the
	// dialog row in the transaction of the last portion.
	var firstErr error
	chat.Walk(func(userId int64, participant *mtproto.ImmutableChatParticipant) error {
		if err := c.deleteChatHistory(userId, chat.Id()); err != nil {
			c.Logger.Errorf("messages.deleteChat - delete the history of user %d in chat %d: %v", userId, chat.Id(), err)
			if firstErr == nil {
				firstErr = err
			}
		}
		return nil
	})
	if firstErr != nil {
		return nil, firstErr
	}

	return mtproto.BoolTrue, nil
}

// deleteChatHistory deletes userId's history of the chat and its dialog row,
// repeating msg.deleteHistory while portions are left.
func (c *ChatsCore) deleteChatHistory(userId, chatId int64) error {
	for {
		r, err := c.svcCtx.Dao.MsgClient.MsgDeleteHistory(c.ctx, &msgpb.TLMsgDeleteHistory{
			UserId:    userId,
			AuthKeyId: 0,
			PeerType:  mtproto.PEER_CHAT,
			PeerId:    chatId,
			JustClear: false,
			Revoke:    false,
		})
		if err != nil {
			return err
		}
		if r.GetOffset() <= 0 {
			return nil
		}
	}
}
