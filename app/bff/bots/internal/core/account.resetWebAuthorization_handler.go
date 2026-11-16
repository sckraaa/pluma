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

// AccountResetWebAuthorization
// account.resetWebAuthorization#2d01b9ef hash:long = Bool;
//
// Logs the user out of one site: the next login there asks again
// (ADR 0068 p. 6). The ID tokens and data a site got stay with it. The
// answer is boolTrue also for a site already gone, removed from another
// device: Android drops the row from the list only on success
// (ui/SessionsActivity.java:553-566), and the row is gone either way.
func (c *BotsCore) AccountResetWebAuthorization(in *mtproto.TLAccountResetWebAuthorization) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Bots.ResetWebAuthorization(c.ctx, c.MD.UserId, in.GetHash()); err != nil {
		c.Logger.Errorf("account.resetWebAuthorization - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
