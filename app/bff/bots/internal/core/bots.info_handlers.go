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
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// optional returns the text of a flag, nil when not sent.
func optional(v *wrapperspb.StringValue) *string {
	if v == nil {
		return nil
	}
	s := v.GetValue()
	return &s
}

func tooLong(v *wrapperspb.StringValue, max int) bool {
	return v != nil && utf8.RuneCountInString(v.GetValue()) > max
}

// BotsSetBotInfo
// bots.setBotInfo#10cf3123 flags:# bot:flags.2?InputUser lang_code:string name:flags.3?string about:flags.0?string description:flags.1?string = Bool;
func (c *BotsCore) BotsSetBotInfo(in *mtproto.TLBotsSetBotInfo) (*mtproto.Bool, error) {
	botId, err := c.asBot(in.Bot)
	if err != nil {
		return nil, err
	}
	if !botstore.ValidLangCode(in.LangCode) {
		return nil, mtproto.ErrLangCodeInvalid
	}
	if tooLong(in.Name, botstore.MaxName) || tooLong(in.About, botstore.MaxAbout) || tooLong(in.Description, botstore.MaxDescription) {
		return nil, mtproto.ErrMessageTooLong
	}
	if in.LangCode == "" {
		// The default language's name and about are the account's own.
		if in.Name != nil {
			if _, err = c.svcCtx.UserUpdateFirstAndLastName(c.ctx, &userpb.TLUserUpdateFirstAndLastName{UserId: botId, FirstName: in.Name.GetValue()}); err != nil {
				return nil, err
			}
		}
		if in.About != nil {
			if _, err = c.svcCtx.UserUpdateAbout(c.ctx, &userpb.TLUserUpdateAbout{UserId: botId, About: in.About.GetValue()}); err != nil {
				return nil, err
			}
		}
	}
	if err = c.svcCtx.Bots.SetTexts(c.ctx, botId, in.LangCode, botstore.Texts{
		Name: optional(in.Name), About: optional(in.About), Description: optional(in.Description),
	}); err != nil {
		c.Logger.Errorf("bots.setBotInfo - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.touchBot(botId); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// getBotInfo is the texts of the bot in lang, each falling back to the
// default one (core.telegram.org/method/bots.setBotInfo: an empty lang_code
// is the fallback text).
func (c *BotsCore) getBotInfo(bot *mtproto.InputUser, lang string) (name, about, description string, err error) {
	botId, err := c.asBot(bot)
	if err != nil {
		return "", "", "", err
	}
	if !botstore.ValidLangCode(lang) {
		return "", "", "", mtproto.ErrLangCodeInvalid
	}
	u, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: botId})
	if err != nil {
		return "", "", "", err
	}
	name, about = u.FirstName(), u.About()
	if description, err = c.svcCtx.Bots.Description(c.ctx, botId); err != nil {
		c.Logger.Errorf("bots.getBotInfo - error: %v", err)
		return "", "", "", mtproto.ErrInternalServerError
	}
	texts, err := c.svcCtx.Bots.LangTexts(c.ctx, botId, lang)
	if err != nil {
		c.Logger.Errorf("bots.getBotInfo - error: %v", err)
		return "", "", "", mtproto.ErrInternalServerError
	}
	if texts.Name != nil {
		name = *texts.Name
	}
	if texts.About != nil {
		about = *texts.About
	}
	if texts.Description != nil {
		description = *texts.Description
	}
	return name, about, description, nil
}

// BotsGetBotInfoDCD914FD
// bots.getBotInfo#dcd914fd flags:# bot:flags.0?InputUser lang_code:string = bots.BotInfo;
func (c *BotsCore) BotsGetBotInfoDCD914FD(in *mtproto.TLBotsGetBotInfoDCD914FD) (*mtproto.Bots_BotInfo, error) {
	name, about, description, err := c.getBotInfo(in.Bot, in.LangCode)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLBotsBotInfo(&mtproto.Bots_BotInfo{Name: name, About: about, Description: description}).To_Bots_BotInfo(), nil
}

// BotsGetBotInfo75EC12E6
// bots.getBotInfo#75ec12e6 lang_code:string = Vector<string>;
func (c *BotsCore) BotsGetBotInfo75EC12E6(in *mtproto.TLBotsGetBotInfo75EC12E6) (*mtproto.Vector_String, error) {
	name, about, description, err := c.getBotInfo(nil, in.LangCode)
	if err != nil {
		return nil, err
	}
	return &mtproto.Vector_String{Datas: []string{name, about, description}}, nil
}

// BotsGetAdminedBots
// bots.getAdminedBots#b0711d83 = Vector<User>;
func (c *BotsCore) BotsGetAdminedBots(in *mtproto.TLBotsGetAdminedBots) (*mtproto.Vector_User, error) {
	ids, err := c.svcCtx.Bots.Admined(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("bots.getAdminedBots - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if len(ids) == 0 {
		return &mtproto.Vector_User{Datas: []*mtproto.User{}}, nil
	}
	users, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: append(append([]int64{}, ids...), c.MD.UserId)})
	if err != nil {
		return nil, err
	}
	return &mtproto.Vector_User{Datas: users.GetUserListByIdList(c.MD.UserId, ids...)}, nil
}

// BotsSetBotMenuButton
// bots.setBotMenuButton#4504d54f user_id:InputUser button:BotMenuButton = Bool;
func (c *BotsCore) BotsSetBotMenuButton(in *mtproto.TLBotsSetBotMenuButton) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	userId, err := menuUser(c.MD.UserId, in.UserId)
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Bots.SetMenuButton(c.ctx, c.MD.UserId, userId, in.Button); err != nil {
		return nil, err
	}
	if err = c.touchBot(c.MD.UserId); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// BotsGetBotMenuButton
// bots.getBotMenuButton#9c60eb28 user_id:InputUser = BotMenuButton;
func (c *BotsCore) BotsGetBotMenuButton(in *mtproto.TLBotsGetBotMenuButton) (*mtproto.BotMenuButton, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	userId, err := menuUser(c.MD.UserId, in.UserId)
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Bots.MenuButton(c.ctx, c.MD.UserId, userId)
}

// menuUser is whose menu button a bot names: 0, the default for all users,
// for inputUserEmpty, the input that names nobody. Telegram's pages do not
// say what inputUserEmpty means here (the brief's decisions to confirm).
func menuUser(selfId int64, in *mtproto.InputUser) (int64, error) {
	if in.GetPredicateName() == mtproto.Predicate_inputUserEmpty {
		return 0, nil
	}
	u := mtproto.FromInputUser(selfId, in)
	if !u.IsUser() {
		return 0, mtproto.ErrUserIdInvalid
	}
	return u.PeerId, nil
}
