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
	"errors"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

// AuthCheckPassword
// auth.checkPassword#d18b4d16 password:InputCheckPasswordSRP = auth.Authorization;
//
// The second step of the login of a user with a 2FA password: auth.signIn,
// a future auth token or a QR login answered SESSION_PASSWORD_NEEDED and
// left the key waiting (pkg/twofa). The SRP proof is checked (M1 in
// constant time, under the limits of wrong passwords); only then is the key
// bound to the user. Errors as Telegram's:
//
//   - PASSWORD_HASH_INVALID, SRP_ID_INVALID, SRP_PASSWORD_CHANGED, FLOOD_WAIT_X;
//   - PASSWORD_EMPTY: the password was removed meanwhile, the login starts over;
//   - AUTH_KEY_UNREGISTERED: no login waits on the key (it never did, or it
//     waited longer than twofa.LoginTTL).
//
// A logged-in key checks its own user's password.
func (c *AuthorizationCore) AuthCheckPassword(in *mtproto.TLAuthCheckPassword) (*mtproto.Auth_Authorization, error) {
	if c.MD.UserId != 0 {
		return c.checkOwnPassword(in)
	}

	login, err := c.svcCtx.Twofa.WaitingLogin(c.ctx, c.MD.PermAuthKeyId)
	if err != nil {
		return nil, err
	}
	if login == nil {
		c.Logger.Errorf("auth.checkPassword - no login waits for the password")
		return nil, mtproto.ErrAuthKeyUnregistered
	}

	if err = c.svcCtx.Twofa.Check(c.ctx, login.UserId, c.MD.PermAuthKeyId, in.GetPassword()); err != nil {
		c.Logger.Errorf("auth.checkPassword - user %d: %v", login.UserId, err)
		if errors.Is(err, twofa.ErrNoPassword) {
			c.svcCtx.Twofa.EndLogin(c.ctx, c.MD.PermAuthKeyId)
		}
		return nil, err
	}

	user, err := c.svcCtx.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: login.UserId,
	})
	if err != nil {
		c.Logger.Errorf("auth.checkPassword - user %d: %v", login.UserId, err)
		return nil, err
	}
	c.Logger.Infof("auth.checkPassword - user %d logged in", login.UserId)
	return c.logIn(user, login.Phone, login.LangCode, true)
}

func (c *AuthorizationCore) checkOwnPassword(in *mtproto.TLAuthCheckPassword) (*mtproto.Auth_Authorization, error) {
	if err := c.svcCtx.Twofa.Check(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, in.GetPassword()); err != nil {
		c.Logger.Errorf("auth.checkPassword - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	user, err := c.svcCtx.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("auth.checkPassword - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
		User: user.ToSelfUser(),
	}).To_Auth_Authorization(), nil
}
