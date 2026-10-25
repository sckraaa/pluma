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

// AccountUpdateDeviceLocked
// account.updateDeviceLocked#38df3532 period:int = Bool;
//
// Keeps the inactivity period after which the pushes of this
// authorization's devices hide message texts (task 009); iOS reports it,
// -1 or 0 when the device has no lock.
func (c *NotificationCore) AccountUpdateDeviceLocked(in *mtproto.TLAccountUpdateDeviceLocked) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 || c.MD.PermAuthKeyId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	period := in.GetPeriod()
	if period < 0 {
		period = 0
	}
	if err := c.svcCtx.PushDevices.SetLockedPeriod(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, period); err != nil {
		c.Logger.Errorf("account.updateDeviceLocked - user %d: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	return mtproto.BoolTrue, nil
}
