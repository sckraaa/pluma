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

// AuthsessionDestroyAuthKey
// authsession.destroyAuthKey auth_key_id:long = Vector<long>;
//
// Removes the key, with the temporary keys and the authorization of a
// permanent key (destroy_auth_key, task 008). No ids back: the key was not
// there (destroy_auth_key_none).
func (c *AuthsessionCore) AuthsessionDestroyAuthKey(in *authsession.TLAuthsessionDestroyAuthKey) (*authsession.Vector_Long, error) {
	removed, err := c.svcCtx.Dao.DestroyAuthKey(c.ctx, in.GetAuthKeyId())
	if err != nil {
		c.Logger.Errorf("authsession.destroyAuthKey - key %d: %v", in.GetAuthKeyId(), err)
		return nil, err
	}
	c.Logger.Infof("authsession.destroyAuthKey - key %d: %d keys removed", in.GetAuthKeyId(), len(removed))
	return &authsession.Vector_Long{Datas: removed}, nil
}
