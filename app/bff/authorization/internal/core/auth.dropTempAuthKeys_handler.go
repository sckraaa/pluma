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
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// AuthDropTempAuthKeys
// auth.dropTempAuthKeys#8e48a188 except_auth_keys:Vector<long> = Bool;
//
// Removes the temporary keys bound to the permanent key of the session but
// those in except_auth_keys (task 008); the gateway stops serving them
// within a minute.
func (c *AuthorizationCore) AuthDropTempAuthKeys(in *mtproto.TLAuthDropTempAuthKeys) (*mtproto.Bool, error) {
	if c.MD.PermAuthKeyId == 0 {
		err := mtproto.ErrAuthKeyPermEmpty
		c.Logger.Errorf("auth.dropTempAuthKeys - error: %v", err)
		return nil, err
	}
	if _, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionDropTempAuthKeys(c.ctx, &authsession.TLAuthsessionDropTempAuthKeys{
		PermAuthKeyId:  c.MD.PermAuthKeyId,
		ExceptAuthKeys: in.GetExceptAuthKeys(),
	}); err != nil {
		c.Logger.Errorf("auth.dropTempAuthKeys - perm key %d: %v", c.MD.PermAuthKeyId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
