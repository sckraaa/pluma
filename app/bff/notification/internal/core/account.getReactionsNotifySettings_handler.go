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

// AccountGetReactionsNotifySettings
// account.getReactionsNotifySettings#6dd654c = ReactionsNotifySettings;
//
// The account's reaction notification settings, the defaults while it has
// set none (task 020). iOS asks for them with the other global notification
// settings and repeats the whole request on any error
// (ManagedGlobalNotificationSettings.swift), so an account always has an
// answer. The session writes #71e4ea58 or, for the layers 178-223, #56e34970
// without poll_votes_notify_from. Only users may ask (core.telegram.org/
// method/account.getReactionsNotifySettings).
func (c *NotificationCore) AccountGetReactionsNotifySettings(in *mtproto.TLAccountGetReactionsNotifySettings) (*mtproto.ReactionsNotifySettings, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.getReactionsNotifySettings - error: %v", err)
		return nil, err
	}
	settings, err := c.svcCtx.Dao.UserClient.UserGetReactionsNotifySettings(c.ctx, &userpb.TLUserGetReactionsNotifySettings{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("account.getReactionsNotifySettings - error: %v", err)
		return nil, err
	}
	return settings, nil
}
