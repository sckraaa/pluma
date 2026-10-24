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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountSetGlobalPrivacySettings
// account.setGlobalPrivacySettings#1edaaac2 settings:GlobalPrivacySettings = GlobalPrivacySettings;
//
// new_noncontact_peers_require_premium (only contacts, chats there already
// and Premium users may write) is a Premium feature: turning it on without
// Premium answers PREMIUM_ACCOUNT_REQUIRED, as Telegram does
// (new_noncontact_peers_require_premium_without_ownpremium is off).
func (c *PrivacySettingsCore) AccountSetGlobalPrivacySettings(in *mtproto.TLAccountSetGlobalPrivacySettings) (*mtproto.GlobalPrivacySettings, error) {
	rSettings := in.GetSettings()
	if rSettings.GetNewNoncontactPeersRequirePremium() {
		me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
		if err != nil {
			c.Logger.Errorf("account.setGlobalPrivacySettings - error: %v", err)
			return nil, err
		}
		if !me.Premium() {
			err = mtproto.ErrPremiumAccountRequired
			c.Logger.Errorf("account.setGlobalPrivacySettings - user %d: %v", c.MD.UserId, err)
			return nil, err
		}
	}

	_, err := c.svcCtx.Dao.UserClient.UserSetGlobalPrivacySettings(c.ctx, &userpb.TLUserSetGlobalPrivacySettings{
		UserId:   c.MD.UserId,
		Settings: rSettings,
	})
	if err != nil {
		c.Logger.Errorf("account.setGlobalPrivacySettings - error: %v", err)
		return nil, err
	}

	return rSettings, nil
}
