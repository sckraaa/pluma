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

// commandKey checks the common part of the three methods of a command list:
// the caller is a bot (USER_BOT_REQUIRED) and the scope and language are
// valid (core.telegram.org/method/bots.setBotCommands).
func (c *BotsCore) commandKey(scope *mtproto.BotCommandScope, lang string) (string, error) {
	if _, err := c.meBot(); err != nil {
		return "", err
	}
	if !botstore.ValidLangCode(lang) {
		return "", mtproto.ErrLangCodeInvalid
	}
	return botstore.ScopeKey(c.MD.UserId, scope)
}

// BotsSetBotCommands
// bots.setBotCommands#517165a scope:BotCommandScope lang_code:string commands:Vector<BotCommand> = Bool;
func (c *BotsCore) BotsSetBotCommands(in *mtproto.TLBotsSetBotCommands) (*mtproto.Bool, error) {
	key, err := c.commandKey(in.Scope, in.LangCode)
	if err != nil {
		return nil, err
	}
	if err = botstore.ValidCommands(in.Commands); err != nil {
		return nil, err
	}
	if err = c.svcCtx.Bots.SetCommands(c.ctx, c.MD.UserId, key, in.LangCode, in.Commands); err != nil {
		c.Logger.Errorf("bots.setBotCommands - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.touchBot(c.MD.UserId); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// BotsResetBotCommands
// bots.resetBotCommands#3d8de0f9 scope:BotCommandScope lang_code:string = Bool;
func (c *BotsCore) BotsResetBotCommands(in *mtproto.TLBotsResetBotCommands) (*mtproto.Bool, error) {
	key, err := c.commandKey(in.Scope, in.LangCode)
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Bots.ResetCommands(c.ctx, c.MD.UserId, key, in.LangCode); err != nil {
		c.Logger.Errorf("bots.resetBotCommands - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.touchBot(c.MD.UserId); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// BotsGetBotCommands
// bots.getBotCommands#e34c0dd6 scope:BotCommandScope lang_code:string = Vector<BotCommand>;
func (c *BotsCore) BotsGetBotCommands(in *mtproto.TLBotsGetBotCommands) (*mtproto.Vector_BotCommand, error) {
	key, err := c.commandKey(in.Scope, in.LangCode)
	if err != nil {
		return nil, err
	}
	commands, err := c.svcCtx.Bots.Commands(c.ctx, c.MD.UserId, key, in.LangCode)
	if err != nil {
		c.Logger.Errorf("bots.getBotCommands - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	return &mtproto.Vector_BotCommand{Datas: commands}, nil
}
