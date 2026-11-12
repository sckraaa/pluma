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
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/newlogin"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicemsg"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicetext"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

// AuthRequestPasswordRecovery
// auth.requestPasswordRecovery#d897bc66 = auth.PasswordRecovery;
//
// Sends a code to the recovery email of the cloud password (pkg/twofa,
// Service.RequestRecovery; ADR 0058), in the session's language, to a
// logged-in session or to a login that gave the phone code and waits for
// the password, as https://core.telegram.org/api/srp allows. The answer is
// the pattern of the address. PASSWORD_RECOVERY_NA without a recovery email
// (or without mail on the server): the clients then offer the reset
// (account.resetPassword when logged in, the reset of the account
// otherwise; pluma-admin password reset stays). FLOOD_WAIT_X past the
// limits or when the provider did not take the email. Web K's "send again"
// is this call again (emailConfirmation.tsx:118-121).
func (c *AuthorizationCore) AuthRequestPasswordRecovery(in *mtproto.TLAuthRequestPasswordRecovery) (*mtproto.Auth_PasswordRecovery, error) {
	userId, _, lang, err := c.recoveryUser()
	if err != nil {
		c.Logger.Errorf("auth.requestPasswordRecovery - %v", err)
		return nil, err
	}
	pattern, err := c.svcCtx.Twofa.RequestRecovery(c.ctx, userId, c.MD.PermAuthKeyId, lang)
	if err != nil {
		c.Logger.Errorf("auth.requestPasswordRecovery - user %d: %v", userId, err)
		return nil, err
	}
	return mtproto.MakeTLAuthPasswordRecovery(&mtproto.Auth_PasswordRecovery{
		EmailPattern: pattern,
	}).To_Auth_PasswordRecovery(), nil
}

// recoveryUser is the user whose password the key may recover: its own, or
// that of the login waiting for the password on it (nil login for a
// logged-in key); lang is the language of the session.
func (c *AuthorizationCore) recoveryUser() (userId int64, login *twofa.Login, lang string, err error) {
	lang = c.MD.GetLangCode()
	if c.MD.UserId != 0 {
		return c.MD.UserId, nil, lang, nil
	}
	if login, err = c.svcCtx.Twofa.WaitingLogin(c.ctx, c.MD.PermAuthKeyId); err != nil {
		return 0, nil, "", err
	}
	if login == nil {
		return 0, nil, "", mtproto.ErrAuthKeyUnregistered
	}
	if lang == "" {
		lang = login.LangCode
	}
	return login.UserId, login, lang, nil
}

// tellPasswordRecovered sends every session of userId the message of the
// service notifications account about the password recovered by email
// (ADR 0058, p. 5), with the device and place of this session, in its
// language. The other sessions are not ended.
func (c *AuthorizationCore) tellPasswordRecovered(userId int64, lang string, date time.Time) error {
	user, err := c.svcCtx.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: userId})
	if err != nil {
		return fmt.Errorf("user: %w", err)
	}
	list, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionGetAuthorizations(c.ctx, &authsession.TLAuthsessionGetAuthorizations{
		UserId:           userId,
		ExcludeAuthKeyId: c.MD.PermAuthKeyId,
	})
	if err != nil {
		return fmt.Errorf("authorizations: %w", err)
	}
	var current *mtproto.Authorization
	for _, a := range list.GetAuthorizations() {
		if a.GetCurrent() {
			current = a
		}
	}
	if current == nil {
		return fmt.Errorf("no authorization of key %d", c.MD.PermAuthKeyId)
	}
	text, entities := servicetext.PasswordRecovered{
		Name:     mtproto.GetUserName(user.ToSelfUser()),
		Date:     date,
		Device:   newlogin.Device(current),
		Location: current.GetCountry(),
	}.Text(lang)
	return servicemsg.Send(c.ctx, c.svcCtx.Dao.MsgClient, userId, date, text, entities)
}
