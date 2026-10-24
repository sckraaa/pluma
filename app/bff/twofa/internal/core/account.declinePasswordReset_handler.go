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

// AccountDeclinePasswordReset
// account.declinePasswordReset#4c9409f6 = Bool;
//
// Cancels the reset of the password asked with account.resetPassword
// (RESET_REQUEST_MISSING if none is pending).
func (c *TwoFaCore) AccountDeclinePasswordReset(in *mtproto.TLAccountDeclinePasswordReset) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	if err := c.svcCtx.Twofa.DeclineReset(c.ctx, c.MD.UserId); err != nil {
		c.Logger.Errorf("account.declinePasswordReset - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
