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

// UserSetGhost
// user.setGhost user_id:long ghost:Bool = Bool;
//
// Makes the account the support ghost or an ordinary account again
// (pluma-admin ghost set, ADR 0048): its phone goes to nobody but itself,
// nobody finds it by the number or adds it to a chat, and anyone may write
// to it (mtproto.ImmutableUser.Ghost). The flag and updateUser to the
// account's devices and to the users who see it are one box transaction
// (Dao.SetGhost). It returns whether the flag changed; an unknown or
// deleted account is USER_ID_INVALID.
func (c *UserCore) UserSetGhost(in *user.TLUserSetGhost) (*mtproto.Bool, error) {
	changed, err := c.svcCtx.Dao.SetGhost(c.ctx, in.GetUserId(), mtproto.FromBool(in.GetGhost()))
	if err != nil {
		c.Logger.Errorf("user.setGhost - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.ToBool(changed), nil
}
