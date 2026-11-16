// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
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

package core

import (
	"github.com/teamgram/proto/mtproto"
)

// AccountConfirmBotConnection
// account.confirmBotConnection#67ed1f68 bot_id:InputUser = Bool;
//
// Confirms a connection of a business bot (task 105, part 6). The clients
// call it from the screen of a new connection that updateNewBotConnection
// opens (Android messenger/UnconfirmedAuthController.java:355-362, iOS
// TelegramEngine/AccountData/BotConnectionReviews.swift:7-30) and take
// boolTrue as confirmed. Pluma sends no updateNewBotConnection: a bot is
// connected by account.updateConnectedBot of the account itself, confirmed
// by that (ADR 0067). So the bot connected to the account is confirmed,
// boolTrue; any other is BOT_INVALID.
func (c *AccountCore) AccountConfirmBotConnection(in *mtproto.TLAccountConfirmBotConnection) (*mtproto.Bool, error) {
	const method = "account.confirmBotConnection"
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	p := mtproto.FromInputUser(c.MD.UserId, in.GetBotId())
	if p.PeerType != mtproto.PEER_USER {
		err := mtproto.ErrBotInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	conn, err := c.svcCtx.Business.Connection(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	if conn == nil || conn.BotID != p.PeerId {
		err = mtproto.ErrBotInvalid
		c.Logger.Errorf("%s - bot %d is not connected: %v", method, p.PeerId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
