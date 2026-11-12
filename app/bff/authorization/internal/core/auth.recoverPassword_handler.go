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
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AuthRecoverPassword
// auth.recoverPassword#37096c70 flags:# code:string new_settings:flags.0?account.PasswordInputSettings = auth.Authorization;
//
// Recovers the cloud password with the code sent to the recovery email
// (pkg/twofa, Service.Recover; ADR 0058): without new_settings the password
// and its recovery email are removed (Web K passwordManager.ts:97-104), with
// them a new password is set. The code is used once. A login waiting for
// the password then logs in, as auth.checkPassword does (iOS
// Authorization.swift:1355, Android LoginActivity.java:7912, tdesktop
// intro_password_check.cpp:243), after the check of a ban; a logged-in
// session gets its own user. Every session of the account is told; none is
// ended (ADR 0058, p. 5).
func (c *AuthorizationCore) AuthRecoverPassword(in *mtproto.TLAuthRecoverPassword) (*mtproto.Auth_Authorization, error) {
	userId, login, lang, err := c.recoveryUser()
	if err != nil {
		c.Logger.Errorf("auth.recoverPassword - %v", err)
		return nil, err
	}
	if login != nil {
		// A ban that came while the login waited for the password (task 025).
		banned, err := c.svcCtx.Moderation.PhoneBanned(c.ctx, login.Phone)
		if err != nil {
			c.Logger.Errorf("auth.recoverPassword - moderation: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
		if banned {
			c.svcCtx.Twofa.EndLogin(c.ctx, c.MD.PermAuthKeyId)
			return nil, mtproto.ErrPhoneNumberBanned
		}
	}

	hasPassword, err := c.svcCtx.Twofa.Recover(c.ctx, userId, c.MD.PermAuthKeyId, lang, in.GetCode(), in.GetNewSettings())
	if err != nil {
		c.Logger.Errorf("auth.recoverPassword - user %d: %v", userId, err)
		return nil, err
	}
	date := time.Now()

	user, err := c.svcCtx.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: userId})
	if err != nil {
		c.Logger.Errorf("auth.recoverPassword - user %d: %v", userId, err)
		return nil, err
	}
	var auth *mtproto.Auth_Authorization
	if login != nil {
		if auth, err = c.logIn(user, login.Phone, login.LangCode, hasPassword); err != nil {
			c.Logger.Errorf("auth.recoverPassword - user %d: the login after the recovery: %v", userId, err)
			return nil, err
		}
		c.Logger.Infof("auth.recoverPassword - user %d logged in", userId)
	} else {
		auth = mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
			User: user.ToSelfUser(),
		}).To_Auth_Authorization()
	}
	if err = c.tellPasswordRecovered(userId, lang, date); err != nil {
		c.Logger.Errorf("auth.recoverPassword - user %d: the message on the recovery: %v", userId, err)
	}
	return auth, nil
}
