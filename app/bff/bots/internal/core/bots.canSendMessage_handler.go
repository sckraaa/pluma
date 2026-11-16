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

// BotsCanSendMessage
// bots.canSendMessage#1359f4e6 bot:InputUser = Bool;
//
// Asked by a mini app (requestWriteAccess of telegram-web-app.js) before it
// asks the user to let the bot write: tdesktop sends it and, on boolFalse,
// asks the user and sends bots.allowSendMessage
// (inline_bots/bot_attach_web_view.cpp:1902-1922). True when the user let
// the bot write (bot_write_allowed, ADR 0068 p. 4) or wrote to it.
func (c *BotsCore) BotsCanSendMessage(in *mtproto.TLBotsCanSendMessage) (*mtproto.Bool, error) {
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	may, err := c.botMayWrite(bot.Id())
	if err != nil {
		c.Logger.Errorf("bots.canSendMessage - bot %d: %v", bot.Id(), err)
		return nil, mtproto.ErrInternalServerError
	}
	return mtproto.ToBool(may), nil
}
