// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
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

package core

import (
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

// MessagesEditChatCreator
// messages.editChatCreator#f743b857 peer:InputPeer user_id:InputUser password:InputCheckPasswordSRP = Updates;
//
// The 2FA password it takes is checked first with the shared code, as
// Telegram does, so clients go through their usual steps: PASSWORD_MISSING
// without a password (the user is asked to set one), PASSWORD_TOO_FRESH_X
// and SESSION_TOO_FRESH_X (twofa.CheckTransfer), PASSWORD_HASH_INVALID for
// inputCheckPasswordEmpty or a wrong one (the user is asked for it),
// FLOOD_WAIT_X past the limits. iOS probes so with inputPeerEmpty
// (ChannelOwnershipTransfer.swift:36-41). A channel or supergroup goes to
// its new owner (task 042, biz channel.editCreator, as channels.editCreator);
// a basic group's transfer is not served (an upstream enterprise feature,
// ERR_ENTERPRISE_IS_BLOCKED).
func (c *ChatsCore) MessagesEditChatCreator(in *mtproto.TLMessagesEditChatCreator) (*mtproto.Updates, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	session, err := c.svcCtx.Dao.AuthsessionGetAuthorization(c.ctx, &authsession.TLAuthsessionGetAuthorization{AuthKeyId: c.MD.PermAuthKeyId})
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Twofa.CheckTransfer(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, in.GetPassword(), int64(session.GetDateCreated())); err != nil {
		if errors.Is(err, twofa.ErrNoPassword) {
			err = mtproto.ErrPasswordMissing
		}
		c.Logger.Errorf("messages.editChatCreator - user %d: %v", c.MD.UserId, err)
		return nil, err
	}

	if in.GetPeer().GetPredicateName() != mtproto.Predicate_inputPeerChannel {
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != peer.AccessHash {
		return nil, mtproto.ErrChannelInvalid
	}
	user := mtproto.FromInputUser(c.MD.UserId, in.UserId)
	if user.PeerType != mtproto.PEER_USER && user.PeerType != mtproto.PEER_SELF {
		return nil, mtproto.ErrUserIdInvalid
	}
	if user.PeerId != c.MD.UserId {
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: []int64{user.PeerId}})
		if err != nil {
			return nil, err
		}
		if u, ok := users.GetImmutableUser(user.PeerId); !ok || u.Deleted() {
			return nil, mtproto.ErrInputUserDeactivated
		}
	}
	return c.svcCtx.Dao.Channel.ChannelEditCreator(c.ctx, &channelpb.TLChannelEditCreator{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     peer.PeerId,
		UserId:        user.PeerId,
	})
}
