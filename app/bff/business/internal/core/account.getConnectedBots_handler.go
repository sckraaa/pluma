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
)

// AccountGetConnectedBots
// account.getConnectedBots#4ea4c80f = account.ConnectedBots;
//
// The connected bot of the account, none or one: tdesktop takes the first
// (data/business/data_business_chatbots.cpp:28-50), iOS reads it with its
// own full user (TelegramEngine/Peers/UpdateCachedPeerData.swift:236) and
// Web K on every opening of the settings
// (components/sidebarLeft/tabs/settings.tsx:301, 328). users holds the bot
// and the users its recipients name, which tdesktop looks up when it reads
// them (data_business_common.cpp, FromMTP).
func (c *BusinessCore) AccountGetConnectedBots(in *mtproto.TLAccountGetConnectedBots) (*mtproto.Account_ConnectedBots, error) {
	const method = "account.getConnectedBots"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	conn, err := c.svcCtx.Dao.Store.Connection(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	bots := []*mtproto.ConnectedBot{}
	var ids []int64
	if conn != nil {
		bots = append(bots, conn.ToTL())
		ids = append([]int64{conn.BotID}, conn.Recipients.UserIDs()...)
	}
	users, err := c.usersFor(method, ids)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLAccountConnectedBots(&mtproto.Account_ConnectedBots{
		ConnectedBots: bots,
		Users:         users,
	}).To_Account_ConnectedBots(), nil
}
