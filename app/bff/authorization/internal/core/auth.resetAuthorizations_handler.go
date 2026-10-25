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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsessionclient "github.com/teamgram/teamgram-server/app/service/authsession/client"
)

// AuthResetAuthorizations
// auth.resetAuthorizations#9fab0d1a = Bool;
//
// Terminates every other session of the user (task 008), as
// account.resetAuthorization terminates one: their authorizations are
// removed and their devices get updateAccountResetAuthorization. A session
// younger than a day may not do it (FRESH_RESET_AUTHORISATION_FORBIDDEN).
func (c *AuthorizationCore) AuthResetAuthorizations(in *mtproto.TLAuthResetAuthorizations) (*mtproto.Bool, error) {
	if err := authsessionclient.CheckResetAllowed(c.ctx, c.svcCtx.Dao.AuthsessionClient, c.MD.UserId, c.MD.PermAuthKeyId, time.Now().Unix()); err != nil {
		c.Logger.Errorf("auth.resetAuthorizations - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	keys, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionResetAuthorization(c.ctx, &authsession.TLAuthsessionResetAuthorization{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		Hash:      0, // every authorization but this one
	})
	if err != nil {
		c.Logger.Errorf("auth.resetAuthorizations - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	for _, id := range keys.GetDatas() {
		// The terminated session drops its state and stops. Its
		// authorization is gone already: a lost push leaves a session that
		// can no longer act as the user.
		for _, updates := range []*mtproto.Updates{
			mtproto.MakeTLUpdatesTooLong(nil).To_Updates(),
			mtproto.MakeTLUpdateAccountResetAuthorization(&mtproto.Updates{
				UserId:    c.MD.UserId,
				AuthKeyId: id,
			}).To_Updates(),
		} {
			if _, err = c.svcCtx.Dao.SyncClient.SyncUpdatesMe(c.ctx, &sync.TLSyncUpdatesMe{
				UserId:        c.MD.UserId,
				PermAuthKeyId: id,
				Updates:       updates,
			}); err != nil {
				c.Logger.Errorf("auth.resetAuthorizations - tell key %d: %v", id, err)
			}
		}
	}
	c.Logger.Infof("auth.resetAuthorizations - user %d: %d sessions terminated", c.MD.UserId, len(keys.GetDatas()))
	return mtproto.BoolTrue, nil
}
