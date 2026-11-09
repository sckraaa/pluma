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

	"google.golang.org/grpc/status"
)

var (
	// errBotFallbackUnsupported: "The fallback flag can't be set for bots"
	// (core.telegram.org/method/photos.updateProfilePhoto).
	errBotFallbackUnsupported = status.Error(mtproto.ErrBadRequest, "BOT_FALLBACK_UNSUPPORTED")
	// errNeedActionMissing: photos.uploadContactProfilePhoto with neither
	// save nor suggest (core.telegram.org/method/photos.uploadContactProfilePhoto).
	errNeedActionMissing = status.Error(mtproto.ErrBadRequest, "NEED_ACTION_MISSING")
	// errContactMissing: a personal or suggested photo for a user who is
	// not the caller's contact (biz user answers the same under its lock).
	errContactMissing = status.Error(mtproto.ErrBadRequest, "CONTACT_MISSING")
)

// photoOwner is whose photo photos.uploadProfilePhoto and
// photos.updateProfilePhoto change: the caller's, or with the bot flag "a bot
// we own" (core.telegram.org/method/photos.uploadProfilePhoto), which the
// clients offer on user.bot_can_edit (Android ChatEditActivity.java:1915-1941)
// and which is BOT_INVALID for any other user. The public photo of a bot is
// BOT_FALLBACK_UNSUPPORTED, as in Telegram.
func (c *UserChannelProfilesCore) photoOwner(bot *mtproto.InputUser, fallback bool) (int64, error) {
	if bot == nil {
		return c.MD.UserId, nil
	}
	if fallback {
		return 0, errBotFallbackUnsupported
	}
	target := mtproto.FromInputUser(c.MD.UserId, bot)
	if !target.IsUser() {
		return 0, mtproto.ErrBotInvalid
	}
	u, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: target.PeerId})
	if err != nil {
		return 0, err
	}
	if !u.IsBot() || u.Id() != c.MD.UserId && !u.BotCanEditFor(c.MD.UserId) {
		return 0, mtproto.ErrBotInvalid
	}
	return target.PeerId, nil
}

// photoUsers is the users of a photos.Photo answer about owner's photo: the
// account as it sees itself, or the bot as its owner sees it.
func (c *UserChannelProfilesCore) photoUsers(owner int64) ([]*mtproto.User, error) {
	if owner == c.MD.UserId {
		return c.selfUsers()
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{owner, c.MD.UserId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	return users.GetUserListByIdList(c.MD.UserId, owner), nil
}

// selfUsers is the users of a photos.Photo answer about the caller's own
// photo: the account as it sees itself now. tdesktop reads them
// (processUsers of photos.photo.users, api_peer_photo.cpp).
func (c *UserChannelProfilesCore) selfUsers() ([]*mtproto.User, error) {
	ids := []int64{c.MD.UserId}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      ids,
		Privacy: true,
		HasTo:   true,
		To:      ids,
	})
	if err != nil {
		return nil, err
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	if !ok {
		return nil, mtproto.ErrUserIdInvalid
	}
	return []*mtproto.User{me.ToSelfUser()}, nil
}
