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
	"github.com/teamgram/proto/mtproto"
)

// AccountResetPassword
// account.resetPassword#9308ce1b = account.ResetPasswordResult;
//
// The reset of a forgotten password from a logged-in session, as Telegram
// does without a recovery email: the first request answers
// account.resetPasswordRequestedWait until a week later, a request after
// that date removes the password (account.resetPasswordOk), one before it
// the same wait; account.declinePasswordReset cancels it (pkg/twofa,
// Service.Reset).
func (c *AuthorizationCore) AccountResetPassword(in *mtproto.TLAccountResetPassword) (*mtproto.Account_ResetPasswordResult, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	r, err := c.svcCtx.Twofa.Reset(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.resetPassword - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return r, nil
}
