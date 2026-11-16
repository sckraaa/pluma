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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserEditUsernames
// user.editUsernames user_id:long action:int username:string active:Bool order:Vector<string> = Bool;
//
// Shows, hides or orders the usernames of the account beside its editable
// one (task 104, Dao.EditUsernames): account.toggleUsername and
// account.reorderUsernames. The change and updateUserName with seq to the
// account's devices and to the users who see it are one box transaction.
// It answers true; a change that changes nothing is USERNAME_NOT_MODIFIED.
func (c *UserCore) UserEditUsernames(in *user.TLUserEditUsernames) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.EditUsernames(c.ctx, in.GetUserId(), in.GetAction(), in.GetUsername(), mtproto.FromBool(in.GetActive()), in.GetOrder()); err != nil {
		c.Logger.Errorf("user.editUsernames - user %d, action %d: %v", in.GetUserId(), in.GetAction(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
