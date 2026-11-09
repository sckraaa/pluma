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

// sendInput is what the three send methods have in common.
type sendInput struct {
	name     string
	peer     *mtproto.InputEncryptedChat
	randomId int64
	data     []byte
	service  bool
	file     *mtproto.InputEncryptedFile
}

// send delivers one message. A repeated random_id gets the first answer and
// no second delivery, even once the chat ended or the peer blocked the
// sender; the file of a repeat is not stored again.
func (c *SecretChatsCore) send(in *sendInput) (*secretchat.Sent, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	if len(in.data) == 0 {
		return nil, mtproto.ErrDataInvalid
	}
	if len(in.data) > maxPayload {
		return nil, mtproto.ErrDataTooLong
	}
	d := c.device()
	store := c.svcCtx.Dao.Store
	chat, err := store.Check(c.ctx, d, in.peer.GetChatId(), in.peer.GetAccessHash())
	if err != nil {
		return nil, c.rpcError(in.name, err)
	}
	sent, err := store.Lookup(c.ctx, d, in.peer.GetChatId(), in.randomId)
	if err != nil {
		return nil, c.rpcError(in.name, err)
	}
	if sent != nil {
		return sent, nil
	}
	if chat.State != secretchat.StateActive {
		return nil, mtproto.ErrEncryptionDeclined
	}
	blocked, err := c.blockedBy(chat.Peer(d).UserId)
	if err != nil {
		return nil, c.rpcError(in.name, err)
	}
	if blocked {
		return nil, errUserIsBlocked
	}
	var file *secretchat.File
	if in.file != nil {
		if file, err = store.ResolveFile(c.ctx, c.svcCtx.Dao.DfsClient, c.MD.UserId, in.file); err != nil {
			return nil, c.rpcError(in.name, err)
		}
	}
	sent, err = store.Send(c.ctx, secretchat.Send{
		From:       d,
		ChatId:     in.peer.GetChatId(),
		AccessHash: in.peer.GetAccessHash(),
		RandomId:   in.randomId,
		Service:    in.service,
		Data:       in.data,
		File:       file,
	})
	if err != nil {
		return nil, c.rpcError(in.name, err)
	}
	if sent.Delivered != nil {
		c.push(sent.To, nil, sent.Delivered.Update())
	}
	return sent, nil
}

// MessagesSendEncrypted
// messages.sendEncrypted#44fa7a15 flags:# silent:flags.0?true peer:InputEncryptedChat random_id:long data:bytes = messages.SentEncryptedMessage;
func (c *SecretChatsCore) MessagesSendEncrypted(in *mtproto.TLMessagesSendEncrypted) (*mtproto.Messages_SentEncryptedMessage, error) {
	sent, err := c.send(&sendInput{name: "messages.sendEncrypted", peer: in.GetPeer(), randomId: in.GetRandomId(), data: in.GetData()})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesSentEncryptedMessage(&mtproto.Messages_SentEncryptedMessage{Date: sent.Date}).To_Messages_SentEncryptedMessage(), nil
}
