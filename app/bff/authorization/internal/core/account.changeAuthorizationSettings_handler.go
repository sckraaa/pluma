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

// AccountChangeAuthorizationSettings
// account.changeAuthorizationSettings#40f48462 flags:# confirmed:flags.3?true hash:long encrypted_requests_disabled:flags.0?Bool call_requests_disabled:flags.1?Bool = Bool;
//
// Changes the settings of another session of the user (task 019): confirmed
// answers "Yes, it's me" to the banner of a new login
// (https://core.telegram.org/api/auth#confirming-login), and every session
// then gets updateNewAuthorization without unconfirmed, on which the
// official clients drop the banner; the two Bool fields set whether the
// session takes new secret chats and calls, an absent one stays as it is.
// The session itself is listed with hash 0 and is not a session hash names:
// HASH_INVALID, as for a hash the user has no session of.
func (c *AuthorizationCore) AccountChangeAuthorizationSettings(in *mtproto.TLAccountChangeAuthorizationSettings) (*mtproto.Bool, error) {
	if in.GetHash() == 0 {
		err := mtproto.ErrHashInvalid
		c.Logger.Errorf("account.changeAuthorizationSettings - user %d, hash 0: %v", c.MD.UserId, err)
		return nil, err
	}
	key, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionChangeAuthorizationSettings(c.ctx, &authsession.TLAuthsessionChangeAuthorizationSettings{
		UserId:                    c.MD.UserId,
		Hash:                      in.GetHash(),
		Confirmed:                 in.GetConfirmed(),
		EncryptedRequestsDisabled: in.GetEncryptedRequestsDisabled(),
		CallRequestsDisabled:      in.GetCallRequestsDisabled(),
	})
	if err != nil {
		c.Logger.Errorf("account.changeAuthorizationSettings - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	if key.GetV() == 0 {
		err = mtproto.ErrHashInvalid
		c.Logger.Errorf("account.changeAuthorizationSettings - user %d has no such session: %v", c.MD.UserId, err)
		return nil, err
	}
	if in.GetConfirmed() {
		if err = c.svcCtx.NewLogin.Reviewed(c.ctx, c.MD.UserId, in.GetHash()); err != nil {
			// The login is confirmed; a device that kept its banner drops it
			// after authorization_autoconfirm_period.
			c.Logger.Errorf("account.changeAuthorizationSettings - user %d, confirmation not told: %v", c.MD.UserId, err)
		}
	}
	return mtproto.BoolTrue, nil
}
