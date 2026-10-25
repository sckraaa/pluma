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
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// AuthLogOut
// auth.logOut#3e72ba19 = auth.LoggedOut;
func (c *AuthorizationCore) AuthLogOut(in *mtproto.TLAuthLogOut) (*mtproto.Auth_LoggedOut, error) {
	// A login waiting for the password on the key ends too.
	c.svcCtx.Twofa.EndLogin(c.ctx, c.MD.PermAuthKeyId)

	// unbind auth_key and user_id
	_, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionUnbindAuthKeyUser(c.ctx, &authsession.TLAuthsessionUnbindAuthKeyUser{
		AuthKeyId: c.MD.PermAuthKeyId,
		UserId:    c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("auth.logOut - error: %v", err)
		return nil, err
	} else {
		if c.svcCtx.Plugin != nil {
			c.svcCtx.Plugin.OnAuthLogout(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId)
		}
	}
	// No pushes to the devices of this authorization any more (task 009):
	// the clients do not unregister them themselves. A failure here leaves
	// rows the push service drops when it finds the key unbound.
	if _, err = c.svcCtx.PushDevices.DeleteAuthorizations(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId); err != nil {
		c.Logger.Errorf("auth.logOut - push devices of user %d: %v", c.MD.UserId, err)
	}

	futureAuthToken := crypto.RandomBytes(64)
	c.svcCtx.Dao.PutFutureAuthToken(c.ctx, futureAuthToken, c.MD.UserId)

	return mtproto.MakeTLAuthLoggedOut(&mtproto.Auth_LoggedOut{
		FutureAuthToken: futureAuthToken,
	}).To_Auth_LoggedOut(), nil
}
