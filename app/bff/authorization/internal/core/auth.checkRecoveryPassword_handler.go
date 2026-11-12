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

// AuthCheckRecoveryPassword
// auth.checkRecoveryPassword#d36bf79 code:string = Bool;
//
// Tells whether code is the live recovery code (pkg/twofa,
// Service.CheckRecovery) without using it up: CODE_EMPTY, CODE_INVALID,
// PASSWORD_RECOVERY_EXPIRED, FLOOD_WAIT_X. Android goes on only on
// boolTrue (TwoStepVerificationSetupActivity.java:1487-1514).
func (c *AuthorizationCore) AuthCheckRecoveryPassword(in *mtproto.TLAuthCheckRecoveryPassword) (*mtproto.Bool, error) {
	userId, _, _, err := c.recoveryUser()
	if err != nil {
		c.Logger.Errorf("auth.checkRecoveryPassword - %v", err)
		return nil, err
	}
	if err = c.svcCtx.Twofa.CheckRecovery(c.ctx, userId, c.MD.PermAuthKeyId, in.GetCode()); err != nil {
		c.Logger.Errorf("auth.checkRecoveryPassword - user %d: %v", userId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
