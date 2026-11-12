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

// AccountCancelPasswordEmail
// account.cancelPasswordEmail#c1cbd5b6 = Bool;
//
// Drops the recovery email waiting for its code and the code (pkg/twofa,
// Service.CancelEmail). A confirmed recovery email stays (ADR 0058, p. 4).
// Web K calls it to change the address (emailConfirmation.tsx:108); Android
// shows no recovery email after it until it reads account.getPassword again
// (TwoStepVerificationSetupActivity.java:1811-1818).
func (c *TwoFaCore) AccountCancelPasswordEmail(in *mtproto.TLAccountCancelPasswordEmail) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	if err := c.svcCtx.Twofa.CancelEmail(c.ctx, c.MD.UserId); err != nil {
		c.Logger.Errorf("account.cancelPasswordEmail - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
