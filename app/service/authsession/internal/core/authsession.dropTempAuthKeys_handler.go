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
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// AuthsessionDropTempAuthKeys
// authsession.dropTempAuthKeys perm_auth_key_id:long except_auth_keys:Vector<long> = Vector<long>;
//
// Removes the temporary keys bound to the permanent key but those in
// except_auth_keys (auth.dropTempAuthKeys, task 008).
func (c *AuthsessionCore) AuthsessionDropTempAuthKeys(in *authsession.TLAuthsessionDropTempAuthKeys) (*authsession.Vector_Long, error) {
	removed, err := c.svcCtx.Dao.DropTempAuthKeys(c.ctx, in.GetPermAuthKeyId(), in.GetExceptAuthKeys())
	if err != nil {
		c.Logger.Errorf("authsession.dropTempAuthKeys - perm key %d: %v", in.GetPermAuthKeyId(), err)
		return nil, err
	}
	c.Logger.Infof("authsession.dropTempAuthKeys - perm key %d: %d temporary keys removed", in.GetPermAuthKeyId(), len(removed))
	return &authsession.Vector_Long{Datas: removed}, nil
}
