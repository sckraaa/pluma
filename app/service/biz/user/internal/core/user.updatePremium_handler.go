// Copyright 2025 Teamgram Authors
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
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserUpdatePremium
// user.updatePremium user_id:long premium:Bool months:flags.1?int = Bool;
//
// Grants Premium (months 0: for good; otherwise added to an end still
// running, or from now) or takes it away (task 014, ADR 0016); the user's
// devices get their user with the new Premium.
func (c *UserCore) UserUpdatePremium(in *user.TLUserUpdatePremium) (*mtproto.Bool, error) {
	change := dao.PremiumChange{
		Grant:  mtproto.FromBool(in.GetPremium()),
		Months: in.GetMonths().GetValue(),
	}
	if _, err := ChangePremium(c.ctx, c.svcCtx, in.GetUserId(), change); err != nil {
		c.Logger.Errorf("user.updatePremium - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
