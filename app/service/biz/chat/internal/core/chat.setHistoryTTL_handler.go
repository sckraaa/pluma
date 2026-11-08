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

// ChatSetHistoryTTL
// chat.setHistoryTTL self_id:long chat_id:long ttl_period:int = Bool;
//
// The members who may change the chat's info may set the period of its new
// messages (tdesktop TTLValidator::can, canEditInformation).
// CHAT_NOT_MODIFIED for the period it has
// (core.telegram.org/method/messages.setHistoryTTL).
func (c *ChatCore) ChatSetHistoryTTL(in *chat.TLChatSetHistoryTTL) (*mtproto.MutableChat, error) {
	mChat, err := c.changeChat(in.SelfId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.SelfId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case !me.CanChangeInfo():
			return mtproto.ErrChatAdminRequired
		case t.Chat.TtlPeriod == in.TtlPeriod:
			return mtproto.ErrChatNotModified
		}
		if _, err := c.svcCtx.Dao.ChatsDAO.UpdateTTLPeriodTx(b.Tx(), in.TtlPeriod, in.ChatId); err != nil {
			return err
		}
		t.Chat.TtlPeriod = in.TtlPeriod
		t.Chat.Version++
		return nil
	})
	if err != nil {
		c.Logger.Errorf("chat.setHistoryTTL - error: %v", err)
		return nil, err
	}
	return mChat, nil
}
