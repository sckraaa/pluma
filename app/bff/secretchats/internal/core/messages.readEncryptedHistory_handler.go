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
	"github.com/teamgram/proto/mtproto"
)

// MessagesReadEncryptedHistory
// messages.readEncryptedHistory#7f4b690a peer:InputEncryptedChat max_date:int = Bool;
func (c *SecretChatsCore) MessagesReadEncryptedHistory(in *mtproto.TLMessagesReadEncryptedHistory) (*mtproto.Bool, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	if in.GetMaxDate() <= 0 {
		return nil, mtproto.ErrMaxDateInvalid
	}
	to, changed, err := c.svcCtx.Dao.Store.ReadHistory(c.ctx, c.device(), in.GetPeer().GetChatId(), in.GetPeer().GetAccessHash(), in.GetMaxDate())
	if err != nil {
		return nil, c.rpcError("messages.readEncryptedHistory", err)
	}
	if changed {
		c.push(to, nil, mtproto.MakeTLUpdateEncryptedMessagesRead(&mtproto.Update{
			ChatId_INT32: in.GetPeer().GetChatId(),
			MaxDate:      in.GetMaxDate(),
			Date_INT32:   now(),
		}).To_Update())
	}
	return mtproto.BoolTrue, nil
}
