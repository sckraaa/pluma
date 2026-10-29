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
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/authsession/internal/dao"
)

// AuthsessionChangeAuthorizationSettings
// authsession.changeAuthorizationSettings flags:# user_id:long hash:long confirmed:flags.3?true encrypted_requests_disabled:flags.0?Bool call_requests_disabled:flags.1?Bool = Int64;
//
// Changes the settings of the user's authorization hash (task 019): confirms
// a new login, or sets whether the session takes new secret chats and
// calls. The permanent key of the authorization comes back, 0 when the user
// has no authorization hash.
func (c *AuthsessionCore) AuthsessionChangeAuthorizationSettings(in *authsession.TLAuthsessionChangeAuthorizationSettings) (*mtproto.Int64, error) {
	change := dao.AuthorizationChange{Confirm: in.GetConfirmed()}
	if b := in.GetEncryptedRequestsDisabled(); b != nil {
		v := mtproto.FromBool(b)
		change.EncryptedRequestsDisabled = &v
	}
	if b := in.GetCallRequestsDisabled(); b != nil {
		v := mtproto.FromBool(b)
		change.CallRequestsDisabled = &v
	}
	authKeyId, err := c.svcCtx.Dao.ChangeAuthorization(c.ctx, in.GetUserId(), in.GetHash(), change)
	if err != nil {
		c.Logger.Errorf("authsession.changeAuthorizationSettings - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return &mtproto.Int64{V: authKeyId}, nil
}
