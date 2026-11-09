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
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

// BotsCore serves one request of the methods of bots.
type BotsCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

// New returns the core of one request.
func New(ctx context.Context, svcCtx *svc.ServiceContext) *BotsCore {
	return &BotsCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// chatPeer is the chat an input peer names. inputPeerSelf, Saved Messages
// (Android MessagesController.getInputPeer, tdesktop data_peer.cpp:2077), is
// the chat with oneself as PEER_USER: the only form of it msg and the boxes
// know.
func (c *BotsCore) chatPeer(in *mtproto.InputPeer) *mtproto.PeerUtil {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in)
	if peer.PeerType == mtproto.PEER_SELF {
		peer.PeerType = mtproto.PEER_USER
	}
	return peer
}

// meBot checks that the caller is a bot account (USER_BOT_REQUIRED).
func (c *BotsCore) meBot() (*mtproto.ImmutableUser, error) {
	me, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return nil, err
	}
	if !me.IsBot() {
		return nil, mtproto.ErrUserBotRequired
	}
	return me, nil
}

// asBot resolves who a method of a bot's settings acts on: the caller, when
// it is a bot and names no other; a bot the caller owns, when it names one
// (core.telegram.org/method/bots.setBotInfo: for a user it "must contain
// the peer of a bot we own", USER_BOT_INVALID otherwise).
func (c *BotsCore) asBot(bot *mtproto.InputUser) (int64, error) {
	if bot == nil {
		me, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
		if err != nil {
			return 0, err
		}
		if !me.IsBot() {
			return 0, mtproto.ErrUserBotInvalid
		}
		return c.MD.UserId, nil
	}
	target := mtproto.FromInputUser(c.MD.UserId, bot)
	if !target.IsUser() {
		return 0, mtproto.ErrBotInvalid
	}
	if target.PeerId == c.MD.UserId {
		if _, err := c.meBot(); err != nil {
			return 0, mtproto.ErrBotInvalid
		}
		return target.PeerId, nil
	}
	creator, ok, err := c.svcCtx.Bots.Creator(c.ctx, target.PeerId)
	if err != nil {
		return 0, err
	}
	if !ok || creator != c.MD.UserId {
		return 0, mtproto.ErrBotInvalid
	}
	return target.PeerId, nil
}

// touchBot raises bot_info_version and drops the cached user: clients see
// the new version on the bot's user and fetch its info again
// (core.telegram.org/api/bots/commands).
func (c *BotsCore) touchBot(botId int64) error {
	_, err := c.svcCtx.UserUpdateBotData(c.ctx, &userpb.TLUserUpdateBotData{BotId: botId})
	return err
}

// botUser returns the user bot with the id, or BOT_INVALID.
func (c *BotsCore) botUser(id int64) (*mtproto.ImmutableUser, error) {
	u, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: id})
	if err != nil {
		return nil, err
	}
	if !u.IsBot() {
		return nil, mtproto.ErrBotInvalid
	}
	if u.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	return u, nil
}

// usersFor is the users a bot's update carries, as the bot sees them.
func (c *BotsCore) usersFor(botId int64, ids ...int64) ([]*mtproto.User, error) {
	users, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: append(append([]int64{}, ids...), botId)})
	if err != nil {
		return nil, err
	}
	return users.GetUserListByIdList(botId, ids...), nil
}
