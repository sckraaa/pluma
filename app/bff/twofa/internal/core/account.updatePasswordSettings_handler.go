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

// AccountUpdatePasswordSettings
// account.updatePasswordSettings#a59b102f password:InputCheckPasswordSRP new_settings:account.PasswordInputSettings = Bool;
//
// Sets, changes or removes the password of the logged-in user and its
// recovery email (pkg/twofa, Service.UpdateIn): the current password is
// checked if set. A new recovery email waits for the code sent to it in
// the session's language, and the answer is EMAIL_UNCONFIRMED_6 with the
// rest of the request applied, as the clients expect (ADR 0058, p. 4); a
// first password given with it comes into force with the email.
func (c *TwoFaCore) AccountUpdatePasswordSettings(in *mtproto.TLAccountUpdatePasswordSettings) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	err := c.svcCtx.Twofa.UpdateIn(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, c.MD.GetLangCode(), in.GetPassword(), in.GetNewSettings())
	waits := twofa.EmailUnconfirmed(err)
	if err != nil && !waits {
		c.Logger.Errorf("account.updatePasswordSettings - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	// Settings with a new_algo that Update took set, changed or removed the
	// password: the account is told, as Telegram tells it. The change is
	// made; a message that does not go out is logged, not the call's error.
	// A first password waiting for its recovery email is not in force yet:
	// the account is told when the email is confirmed.
	tell := in.GetNewSettings().GetNewAlgo() != nil
	if tell && waits {
		has, hasErr := c.svcCtx.Twofa.HasPassword(c.ctx, c.MD.UserId)
		if hasErr != nil {
			c.Logger.Errorf("account.updatePasswordSettings - user %d: %v", c.MD.UserId, hasErr)
		}
		tell = has || hasErr != nil
	}
	if tell {
		if err := c.tellPasswordChanged(time.Now()); err != nil {
			c.Logger.Errorf("account.updatePasswordSettings - user %d: the message on the change: %v", c.MD.UserId, err)
		}
	}
	if waits {
		c.Logger.Infof("account.updatePasswordSettings - user %d: the recovery email waits for its code", c.MD.UserId)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// tellPasswordChanged sends the account the message of the service
// notifications account on a change of its password (task 022), with the
// device and place of the session that made it, in that session's
// language.
func (c *TwoFaCore) tellPasswordChanged(date time.Time) error {
	user, err := c.svcCtx.User.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return fmt.Errorf("user: %w", err)
	}
	list, err := c.svcCtx.Authsession.AuthsessionGetAuthorizations(c.ctx, &authsession.TLAuthsessionGetAuthorizations{
		UserId:           c.MD.UserId,
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
	text, entities := servicetext.TwoStepChanged{
		Name:     mtproto.GetUserName(user.ToSelfUser()),
		Date:     date,
		Device:   newlogin.Device(current),
		Location: current.GetCountry(),
	}.Text(servicetext.LangOf(c.MD.GetLangCode()))
	return servicemsg.Send(c.ctx, c.svcCtx.Msg, c.MD.UserId, date, text, entities)
}
