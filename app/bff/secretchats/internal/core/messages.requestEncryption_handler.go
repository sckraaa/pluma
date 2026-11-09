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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// MessagesRequestEncryption
// messages.requestEncryption#f64daf43 user_id:InputUser random_id:int g_a:bytes = EncryptedChat;
//
// The chat is the requesting authorization's. The request reaches every
// authorization of the participant that takes them
// (core.telegram.org/api/end-to-end).
func (c *SecretChatsCore) MessagesRequestEncryption(in *mtproto.TLMessagesRequestEncryption) (*mtproto.EncryptedChat, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	peer := mtproto.FromInputUser(c.MD.UserId, in.GetUserId())
	if peer.PeerType != mtproto.PEER_USER || peer.PeerId == c.MD.UserId {
		return nil, mtproto.ErrUserIdInvalid
	}
	if !secretchat.ValidDH(in.GetGA()) {
		return nil, mtproto.ErrDhGAInvalid
	}
	user, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: peer.PeerId})
	if err != nil || user == nil {
		return nil, mtproto.ErrUserIdInvalid
	}
	if user.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	if user.IsBot() {
		return nil, mtproto.ErrUserIdInvalid
	}
	blocked, err := c.blockedBy(peer.PeerId)
	if err != nil {
		return nil, c.rpcError("messages.requestEncryption", err)
	}
	if blocked {
		return nil, errUserIsBlocked
	}
	me := c.device()
	chat, created, err := c.svcCtx.Dao.Store.Request(c.ctx, secretchat.Request{
		Admin:         me,
		ParticipantId: peer.PeerId,
		RandomId:      in.GetRandomId(),
		GA:            in.GetGA(),
	})
	if err != nil {
		return nil, c.rpcError("messages.requestEncryption", err)
	}
	if created {
		keys, err := c.svcCtx.Dao.Store.Devices(c.ctx, peer.PeerId, true)
		if err != nil {
			// The authorizations get it with their next difference.
			c.Logger.Errorf("messages.requestEncryption - devices of user %d: %v", peer.PeerId, err)
		}
		users := c.usersOf(peer.PeerId, c.MD.UserId, peer.PeerId)
		for _, key := range keys {
			d := secretchat.Device{UserId: peer.PeerId, AuthKey: key}
			c.push(d, users, chat.UpdateEncryption(d, now()))
		}
	}
	return chat.View(me), nil
}
