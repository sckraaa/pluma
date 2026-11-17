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

// MessagesGetAttachMenuBot
// messages.getAttachMenuBot#77216192 bot:InputUser = AttachMenuBotsBot;
//
// The attachment menu entry of a bot, inactive while the account has not
// added it (core.telegram.org/api/bots/attach). The clients ask for it on a
// startattach link or from the bot's profile and offer to add an inactive
// entry, then call messages.toggleBotInAttachMenu: tdesktop
// inline_bots/bot_attach_web_view.cpp:2760-2795, Android
// ui/LaunchActivity.java:4474-4560, iOS AttachMenuBots.swift:534-600. iOS
// takes the bot only from users (AttachMenuBots.swift:553-563), so the bot
// goes there. A bot with no entry is BOT_INVALID.
func (c *BotsCore) MessagesGetAttachMenuBot(in *mtproto.TLMessagesGetAttachMenuBot) (*mtproto.AttachMenuBotsBot, error) {
	botId, err := c.attachMenuTarget(in.GetBot())
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBot - error: %v", err)
		return nil, err
	}
	m, err := c.svcCtx.Bots.AttachMenu(c.ctx, botId)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBot - entry of bot %d: %v", botId, err)
		return nil, err
	}
	if m == nil {
		err = mtproto.ErrBotInvalid
		c.Logger.Errorf("messages.getAttachMenuBot - bot %d has no entry: %v", botId, err)
		return nil, err
	}
	installed, err := c.svcCtx.Bots.AttachMenuInstalled(c.ctx, c.MD.UserId, botId)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBot - addition of bot %d: %v", botId, err)
		return nil, err
	}
	docs, err := c.attachMenuDocuments(*m)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBot - icons of bot %d: %v", botId, err)
		return nil, err
	}
	users, err := c.usersAsSeen(botId)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBot - users: %v", err)
		return nil, err
	}
	return mtproto.MakeTLAttachMenuBotsBot(&mtproto.AttachMenuBotsBot{
		Bot:   c.attachMenuBot(m, docs, !installed),
		Users: users,
	}).To_AttachMenuBotsBot(), nil
}

// attachMenuTarget resolves the bot of a method of the attachment menu:
// BOT_METHOD_INVALID for a bot calling it (the menu is a user's, as for
// messages.getAttachMenuBots), BOT_INVALID for anyone but a bot.
func (c *BotsCore) attachMenuTarget(in *mtproto.InputUser) (int64, error) {
	if c.MD.IsBot {
		return 0, mtproto.ErrBotMethodInvalid
	}
	if in == nil {
		return 0, mtproto.ErrBotInvalid
	}
	target := mtproto.FromInputUser(c.MD.UserId, in)
	if !target.IsUser() || target.PeerId == c.MD.UserId {
		return 0, mtproto.ErrBotInvalid
	}
	if _, err := c.botUser(target.PeerId); err != nil {
		return 0, err
	}
	return target.PeerId, nil
}
