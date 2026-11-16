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
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesGetBotApp
// messages.getBotApp#34fdc5c3 app:InputBotApp hash:long = messages.BotApp;
//
// A direct link mini app of a bot (pluma.chat/<bot>/<short name>,
// core.telegram.org/api/links#direct-mini-app-links), before the client
// opens it with messages.requestAppWebView (core.telegram.org/api/bots/webapps#direct-link-mini-apps):
// inactive until the user first opens it, which the clients confirm;
// request_write_access, which adds the "allow messages" choice to that
// confirmation. botApp.hash is the version of the app (bot_apps.version):
// with the hash the client has, the app is botAppNotModified.
func (c *BotsCore) MessagesGetBotApp(in *mtproto.TLMessagesGetBotApp) (*mtproto.Messages_BotApp, error) {
	app, err := c.resolveApp(in.GetApp())
	if err != nil {
		return nil, err
	}
	launched, err := c.svcCtx.Bots.AppLaunched(c.ctx, c.MD.UserId, app.Id)
	if err != nil {
		c.Logger.Errorf("messages.getBotApp - launches of app %d: %v", app.Id, err)
		return nil, mtproto.ErrInternalServerError
	}
	hasSettings, err := c.hasSettings(app.BotId)
	if err != nil {
		return nil, err
	}
	r := &mtproto.Messages_BotApp{
		Inactive:           !launched,
		RequestWriteAccess: app.RequestWriteAccess,
		HasSettings:        hasSettings,
	}
	if in.GetHash() != 0 && in.GetHash() == int64(app.Version) {
		r.App = mtproto.MakeTLBotAppNotModified(nil).To_BotApp()
	} else if r.App, err = c.botAppOf(app); err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesBotApp(r).To_Messages_BotApp(), nil
}

// resolveApp is the app an InputBotApp names: inputBotAppID by its id and
// access hash, inputBotAppShortName by the bot and the short name.
// BOT_APP_INVALID for none (method/messages.getBotApp).
func (c *BotsCore) resolveApp(in *mtproto.InputBotApp) (*botstore.App, error) {
	var (
		app *botstore.App
		err error
	)
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputBotAppID:
		app, err = c.svcCtx.Bots.AppById(c.ctx, in.GetId(), in.GetAccessHash())
	case mtproto.Predicate_inputBotAppShortName:
		bot, berr := c.resolveBot(in.GetBotId())
		if berr != nil {
			return nil, berr
		}
		app, err = c.svcCtx.Bots.AppByShortName(c.ctx, bot.Id(), in.GetShortName())
	default:
		return nil, mtproto.ErrBotAppInvalid
	}
	if err != nil {
		c.Logger.Errorf("bot app - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if app == nil {
		return nil, mtproto.ErrBotAppInvalid
	}
	return app, nil
}

// hasSettings tells whether the bot has the settings item of its
// attachment menu entry (has_settings of messages.botApp and
// tgWebAppShowSettings). Telegram keeps the item with the bot
// (attachMenuBot.has_settings); a bot without an entry has none.
func (c *BotsCore) hasSettings(botId int64) (bool, error) {
	m, err := c.svcCtx.Bots.AttachMenu(c.ctx, botId)
	if err != nil {
		c.Logger.Errorf("bot app - attachment menu of bot %d: %v", botId, err)
		return false, mtproto.ErrInternalServerError
	}
	return m != nil && m.HasSettings, nil
}

// botAppOf is the botApp of an app, with its photo and animation.
func (c *BotsCore) botAppOf(app *botstore.App) (*mtproto.BotApp, error) {
	photo, err := c.svcCtx.MediaGetPhoto(c.ctx, &mediapb.TLMediaGetPhoto{PhotoId: app.PhotoId})
	if err != nil {
		c.Logger.Errorf("bot app - photo of app %d: %v", app.Id, err)
		return nil, err
	}
	a := &mtproto.BotApp{
		Id:          app.Id,
		AccessHash:  app.AccessHash,
		ShortName:   app.ShortName,
		Title:       app.Title,
		Description: app.Description,
		Photo:       photo,
		Hash:        int64(app.Version),
	}
	if app.DocumentId != 0 {
		if a.Document, err = c.svcCtx.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: app.DocumentId}); err != nil {
			c.Logger.Errorf("bot app - animation of app %d: %v", app.Id, err)
			return nil, err
		}
	}
	return mtproto.MakeTLBotApp(a).To_BotApp(), nil
}
