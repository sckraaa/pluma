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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountSetReactionsNotifySettings
// account.setReactionsNotifySettings#316ce548 settings:ReactionsNotifySettings = ReactionsNotifySettings;
//
// The settings replace the account's reaction notification settings and
// come back as stored: Web K and Desktop take the answer as the new state
// (task 020). An absent *_notify_from field is nobody, as the schema says;
// the sound is kept as sent. reactionsNotifySettings#56e34970 of the layers
// 178-223 has no poll_votes_notify_from, so with it the stored poll votes
// setting stays. Settings the server cannot keep are SETTINGS_INVALID. No
// update tells the other devices: the schema has none, and the clients ask
// again. Only users may set them (core.telegram.org/method/
// account.setReactionsNotifySettings).
func (c *NotificationCore) AccountSetReactionsNotifySettings(in *mtproto.TLAccountSetReactionsNotifySettings) (*mtproto.ReactionsNotifySettings, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.setReactionsNotifySettings - error: %v", err)
		return nil, err
	}
	settings := in.GetSettings()
	if err := userpb.CheckReactionsNotifySettings(settings); err != nil {
		c.Logger.Errorf("account.setReactionsNotifySettings - error: %v", err)
		return nil, err
	}
	stored, err := c.svcCtx.Dao.UserClient.UserSetReactionsNotifySettings(c.ctx, &userpb.TLUserSetReactionsNotifySettings{
		UserId:        c.MD.UserId,
		Settings:      settings,
		KeepPollVotes: mtproto.ToBool(keepPollVotes(settings)),
	})
	if err != nil {
		c.Logger.Errorf("account.setReactionsNotifySettings - error: %v", err)
		return nil, err
	}
	return stored, nil
}

// keepPollVotes tells whether settings leave the stored poll votes setting
// as it is: reactionsNotifySettings#56e34970 cannot carry it.
func keepPollVotes(settings *mtproto.ReactionsNotifySettings) bool {
	return settings.GetConstructor() == mtproto.CRC32_reactionsNotifySettings_56e34970
}
