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

// AccountResetWebAuthorizations
// account.resetWebAuthorizations#682d2594 = Bool;
//
// Logs the user out of all the sites logged in on through bots; Android
// reports success only on boolTrue (ui/SessionsActivity.java:428-438).
func (c *BotsCore) AccountResetWebAuthorizations(in *mtproto.TLAccountResetWebAuthorizations) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Bots.ResetWebAuthorizations(c.ctx, c.MD.UserId); err != nil {
		c.Logger.Errorf("account.resetWebAuthorizations - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
