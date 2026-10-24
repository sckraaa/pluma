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

// AccountGetPasswordSettings
// account.getPasswordSettings#9cd4eaf9 password:InputCheckPasswordSRP = account.PasswordSettings;
//
// Checks the password of the logged-in user (clients ask for it before
// they open the 2FA settings) and returns the settings: no recovery email
// and no Telegram Passport, which Pluma does not have.
func (c *TwoFaCore) AccountGetPasswordSettings(in *mtproto.TLAccountGetPasswordSettings) (*mtproto.Account_PasswordSettings, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	r, err := c.svcCtx.Twofa.Settings(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, in.GetPassword())
	if err != nil {
		c.Logger.Errorf("account.getPasswordSettings - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return r, nil
}
