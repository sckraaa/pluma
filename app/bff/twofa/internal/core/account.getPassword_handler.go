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
)

// AccountGetPassword
// account.getPassword#548a30f5 = account.Password;
//
// The password of the logged-in user or, on a key whose login waits for the
// password (SESSION_PASSWORD_NEEDED), of the user logging in: the client
// needs its algorithm, hint and srp_id to call auth.checkPassword. Other
// keys get AUTH_KEY_UNREGISTERED.
func (c *TwoFaCore) AccountGetPassword(in *mtproto.TLAccountGetPassword) (*mtproto.Account_Password, error) {
	userId, err := c.passwordUser()
	if err != nil {
		c.Logger.Errorf("account.getPassword - error: %v", err)
		return nil, err
	}
	r, err := c.svcCtx.Twofa.Password(c.ctx, userId)
	if err != nil {
		c.Logger.Errorf("account.getPassword - user %d: %v", userId, err)
		return nil, err
	}
	return r, nil
}

// passwordUser is the user whose password the key may ask about: its own,
// or that of the login waiting for the password on it.
func (c *TwoFaCore) passwordUser() (int64, error) {
	if c.MD.UserId != 0 {
		return c.MD.UserId, nil
	}
	l, err := c.svcCtx.Twofa.WaitingLogin(c.ctx, c.MD.PermAuthKeyId)
	if err != nil {
		return 0, err
	}
	if l == nil {
		return 0, mtproto.ErrAuthKeyUnregistered
	}
	return l.UserId, nil
}
