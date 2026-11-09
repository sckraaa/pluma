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

// MessagesAcceptEncryption
// messages.acceptEncryption#3dbc0415 peer:InputEncryptedChat g_b:bytes key_fingerprint:long = EncryptedChat;
//
// The accepting authorization keeps the chat; the participant's others are
// told it is discarded, the requester gets encryptedChat
// (core.telegram.org/api/end-to-end). The answer repeats if the first was
// lost.
func (c *SecretChatsCore) MessagesAcceptEncryption(in *mtproto.TLMessagesAcceptEncryption) (*mtproto.EncryptedChat, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	if !secretchat.ValidDH(in.GetGB()) {
		// The documentation lists no error for g_b.
		return nil, mtproto.ErrDhGAInvalid
	}
	me := c.device()
	chat, repeated, err := c.svcCtx.Dao.Store.Accept(c.ctx, secretchat.Accept{
		Participant:    me,
		ChatId:         in.GetPeer().GetChatId(),
		AccessHash:     in.GetPeer().GetAccessHash(),
		GB:             in.GetGB(),
		KeyFingerprint: in.GetKeyFingerprint(),
	})
	if err != nil {
		return nil, c.rpcError("messages.acceptEncryption", err)
	}
	if !repeated {
		admin := secretchat.Device{UserId: chat.AdminId, AuthKey: chat.AdminAuthKey}
		c.push(admin, c.usersOf(chat.AdminId, chat.AdminId, chat.ParticipantId), chat.UpdateEncryption(admin, now()))
		keys, err := c.svcCtx.Dao.Store.Devices(c.ctx, chat.ParticipantId, false)
		if err != nil {
			// The authorizations are told by their next difference.
			c.Logger.Errorf("messages.acceptEncryption - devices of user %d: %v", chat.ParticipantId, err)
		}
		for _, key := range keys {
			if key != me.AuthKey {
				d := secretchat.Device{UserId: chat.ParticipantId, AuthKey: key}
				c.push(d, nil, chat.UpdateEncryption(d, now()))
			}
		}
	}
	return chat.View(me), nil
}
