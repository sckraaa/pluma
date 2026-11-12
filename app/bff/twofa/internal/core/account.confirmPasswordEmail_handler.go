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
	"time"

	"github.com/teamgram/proto/mtproto"
)

// AccountConfirmPasswordEmail
// account.confirmPasswordEmail#8fdf1920 code:string = Bool;
//
// Confirms the recovery email waiting for its code (pkg/twofa,
// Service.ConfirmEmail; ADR 0058): CODE_EMPTY, CODE_INVALID,
// EMAIL_HASH_EXPIRED, FLOOD_WAIT_X. Android sends it with
// RequestFlagWithoutLogin from a logged-in session; the user is the key's,
// as for every other method. A confirmed email is a change of the 2FA
// settings: the account is told, as on a change of the password.
func (c *TwoFaCore) AccountConfirmPasswordEmail(in *mtproto.TLAccountConfirmPasswordEmail) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	if err := c.svcCtx.Twofa.ConfirmEmail(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, in.GetCode()); err != nil {
		c.Logger.Errorf("account.confirmPasswordEmail - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	if err := c.tellPasswordChanged(time.Now()); err != nil {
		c.Logger.Errorf("account.confirmPasswordEmail - user %d: the message on the change: %v", c.MD.UserId, err)
	}
	return mtproto.BoolTrue, nil
}
