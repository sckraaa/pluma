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
	return mtproto.BoolTrue, nil
}
