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
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesToggleBotInAttachMenu
// messages.toggleBotInAttachMenu#69f59d69 flags:# write_allowed:flags.0?true bot:InputUser enabled:Bool = Bool;
//
// Adds the bot's attachment menu entry to the account's menu or removes it
// (core.telegram.org/api/bots/attach; ADR 0068, p. 5). Adding a bot with no
// entry is BOT_INVALID; removing is allowed whatever the entry, so a menu
// keeps no bot the account cannot take out. write_allowed, which the
// clients send when the user ticks "allow messages" in the confirmation
// (tdesktop inline_bots/bot_attach_web_view.cpp:2972-2982, iOS
// AttachMenuBots.swift:398-402), lets the bot write to the user and posts
// messageActionBotAllowed{attach_menu} in the chat with the bot once
// (tdesktop shows it as lng_action_attach_menu_bot_allowed,
// history/history_item.cpp:6528-6530).
//
// Every session of the account gets updateAttachMenuBots, with seq: the
// other devices load the list again (core.telegram.org/api/bots/attach),
// and the one of the request does as well after the answer (tdesktop
// :2983-2986, iOS AttachMenuBots.swift:414-420). It goes on each success,
// a repeat included: a request that failed after the change and is sent
// again still tells the other devices. The change goes first, then the
// update, then the permission: a failure after the change is answered
// with the error and the repeat finishes the rest.
func (c *BotsCore) MessagesToggleBotInAttachMenu(in *mtproto.TLMessagesToggleBotInAttachMenu) (*mtproto.Bool, error) {
	botId, err := c.attachMenuTarget(in.GetBot())
	if err != nil {
		c.Logger.Errorf("messages.toggleBotInAttachMenu - error: %v", err)
		return nil, err
	}
	enable := mtproto.FromBool(in.GetEnabled())
	if enable {
		_, err = c.svcCtx.Bots.InstallAttachMenu(c.ctx, c.MD.UserId, botId, int32(time.Now().Unix()))
		if errors.Is(err, botstore.ErrNoAttachMenu) {
			err = mtproto.ErrBotInvalid
		}
	} else {
		_, err = c.svcCtx.Bots.UninstallAttachMenu(c.ctx, c.MD.UserId, botId)
	}
	if err != nil {
		c.Logger.Errorf("messages.toggleBotInAttachMenu - bot %d, enabled %v: %v", botId, enable, err)
		return nil, err
	}
	if err = c.svcCtx.UserSeq.PushSeq(c.ctx, c.MD.UserId, mtproto.MakeTLUpdateAttachMenuBots(nil).To_Update(), []*mtproto.User{}, []*mtproto.Chat{}); err != nil {
		c.Logger.Errorf("messages.toggleBotInAttachMenu - update of the menu: %v", err)
		return nil, err
	}
	if enable && in.GetWriteAllowed() {
		action := mtproto.MakeTLMessageActionBotAllowed(&mtproto.MessageAction{AttachMenu: true}).To_MessageAction()
		if _, err = c.allowWrite(botId, botstore.WriteAllowedAttachMenu, action); err != nil {
			c.Logger.Errorf("messages.toggleBotInAttachMenu - permission of bot %d: %v", botId, err)
			return nil, err
		}
	}
	return mtproto.BoolTrue, nil
}
