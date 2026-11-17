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
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// BotsAllowSendMessage
// bots.allowSendMessage#f132e3ef bot:InputUser = Updates;
//
// The user lets the bot write to it, asked by a mini app
// (requestWriteAccess). The chat with the bot gets
// messageActionBotAllowed(from_request) once (ADR 0068 p. 4), and the
// answer is the Updates of that message, which tdesktop applies
// (inline_bots/bot_attach_web_view.cpp:1902-1922); updates with nothing in
// them when the bot was let write before.
func (c *BotsCore) BotsAllowSendMessage(in *mtproto.TLBotsAllowSendMessage) (*mtproto.Updates, error) {
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	action := mtproto.MakeTLMessageActionBotAllowed(&mtproto.MessageAction{FromRequest: true}).To_MessageAction()
	updates, err := c.allowWrite(bot.Id(), botstore.WriteAllowedFromRequest, action)
	if err != nil {
		c.Logger.Errorf("bots.allowSendMessage - bot %d: %v", bot.Id(), err)
		return nil, err
	}
	if updates == nil {
		return mtproto.MakeEmptyUpdates(), nil
	}
	return updates, nil
}
