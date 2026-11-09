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
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// MessagesSetEncryptedTyping
// messages.setEncryptedTyping#791451ed peer:InputEncryptedChat typing:Bool = Bool;
//
// updateEncryptedChatTyping has no "stopped" form: boolFalse tells nobody,
// the clients drop the action after a timeout.
func (c *SecretChatsCore) MessagesSetEncryptedTyping(in *mtproto.TLMessagesSetEncryptedTyping) (*mtproto.Bool, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	d := c.device()
	chat, err := c.svcCtx.Dao.Store.Check(c.ctx, d, in.GetPeer().GetChatId(), in.GetPeer().GetAccessHash())
	if err != nil {
		return nil, c.rpcError("messages.setEncryptedTyping", err)
	}
	if !mtproto.FromBool(in.GetTyping()) || chat.State != secretchat.StateActive {
		return mtproto.BoolTrue, nil
	}
	c.push(chat.Peer(d), nil, mtproto.MakeTLUpdateEncryptedChatTyping(&mtproto.Update{ChatId_INT32: chat.Id}).To_Update())
	return mtproto.BoolTrue, nil
}
