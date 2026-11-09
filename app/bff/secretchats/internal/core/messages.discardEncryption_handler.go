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

// MessagesDiscardEncryption
// messages.discardEncryption#f393aea0 flags:# delete_history:flags.0?true chat_id:int = Bool;
func (c *SecretChatsCore) MessagesDiscardEncryption(in *mtproto.TLMessagesDiscardEncryption) (*mtproto.Bool, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	chat, wasRequested, err := c.svcCtx.Dao.Store.Discard(c.ctx, secretchat.Discard{
		By:             c.device(),
		ChatId:         in.GetChatId(),
		HistoryDeleted: in.GetDeleteHistory(),
	})
	if err != nil {
		return nil, c.rpcError("messages.discardEncryption", err)
	}
	targets, err := c.svcCtx.Dao.Store.DiscardTargets(c.ctx, chat, wasRequested)
	if err != nil {
		// The other ends are told by their next difference.
		c.Logger.Errorf("messages.discardEncryption - chat %d: %v", chat.Id, err)
	}
	for _, d := range targets {
		c.push(d, nil, chat.UpdateEncryption(d, now()))
	}
	return mtproto.BoolTrue, nil
}
