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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatToggleNoForwards
// chat.toggleNoForwards chat_id:long operator_id:long enabled:Bool = MutableChat;
//
// Only the creator turns the protection of the content on or off, in its
// box transaction with the chat locked (ADR 0021).
func (c *ChatCore) ChatToggleNoForwards(in *chat.TLChatToggleNoForwards) (*mtproto.MutableChat, error) {
	enabled := mtproto.FromBool(in.Enabled)
	chat2, err := c.changeChat(in.OperatorId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.OperatorId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case !me.IsChatMemberCreator():
			return mtproto.ErrChatAdminRequired
		case t.Chat.Noforwards == enabled:
			return mtproto.ErrChatNotModified
		}
		if _, err := c.svcCtx.Dao.ChatsDAO.UpdateNoforwardsTx(b.Tx(), enabled, in.ChatId); err != nil {
			return err
		}
		t.Chat.Noforwards = enabled
		t.Chat.Version++
		return nil
	})
	if err != nil {
		c.Logger.Errorf("chat.toggleNoForwards - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
