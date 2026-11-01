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
)

// AccountUpdatePasswordSettings
// account.updatePasswordSettings#a59b102f password:InputCheckPasswordSRP new_settings:account.PasswordInputSettings = Bool;
//
// Sets, changes or removes the password of the logged-in user (pkg/twofa,
// Service.Update): the current one is checked if set. A recovery email
// cannot be set (EMAIL_INVALID; an empty one, the client's "skip", is
// fine): there is no mail on the server.
func (c *TwoFaCore) AccountUpdatePasswordSettings(in *mtproto.TLAccountUpdatePasswordSettings) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	if err := c.svcCtx.Twofa.Update(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, in.GetPassword(), in.GetNewSettings()); err != nil {
		c.Logger.Errorf("account.updatePasswordSettings - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	// Settings with a new_algo that Update took set, changed or removed the
	// password: the account is told, as Telegram tells it. The change is
	// made; a message that does not go out is logged, not the call's error.
	if in.GetNewSettings().GetNewAlgo() != nil {
		if err := c.tellPasswordChanged(time.Now()); err != nil {
			c.Logger.Errorf("account.updatePasswordSettings - user %d: the message on the change: %v", c.MD.UserId, err)
		}
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
