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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesRequestAppWebView53618BCE
// messages.requestAppWebView#53618bce flags:# write_allowed:flags.0?true compact:flags.7?true fullscreen:flags.8?true peer:InputPeer app:InputBotApp start_param:flags.1?string theme_params:flags.2?DataJSON platform:string = WebViewResult;
//
// Opens a direct link mini app (core.telegram.org/api/bots/webapps#direct-link-mini-apps)
// after messages.getBotApp, from the chat the link was opened in (tdesktop
// inline_bots/bot_attach_web_view.cpp:1348-1378, Android
// ui/bots/BotWebViewAttachedSheet.java:1259-1295). write_allowed, the
// user's choice in the confirmation, lets the bot write to the user, with
// messageActionBotAllowed(app) in their chat (ADR 0068 p. 4); the first
// launch makes the app active (messages.botApp.inactive). The initData has
// chat_type and chat_instance of the chat, and the bot may answer into it
// when the user may write there (query_id, ADR 0068 p. 4).
func (c *BotsCore) MessagesRequestAppWebView53618BCE(in *mtproto.TLMessagesRequestAppWebView53618BCE) (*mtproto.WebViewResult, error) {
	params, err := webViewParamsOf(in.GetPlatform(), in.GetThemeParams(), in.GetStartParam(), in.GetFullscreen())
	if err != nil {
		return nil, err
	}
	app, err := c.resolveApp(in.GetApp())
	if err != nil {
		return nil, err
	}
	bot, err := c.botUser(app.BotId)
	if errors.Is(err, mtproto.ErrBotInvalid) || errors.Is(err, mtproto.ErrInputUserDeactivated) || errors.Is(err, mtproto.ErrUserIdInvalid) {
		// The app of a bot deleted since.
		return nil, mtproto.ErrBotAppInvalid
	}
	if err != nil {
		return nil, err
	}
	if err = checkWebAppURL(app.URL); err != nil {
		return nil, err
	}
	// The keys first: an app that cannot open changes nothing.
	if _, err = c.webAppKey(bot.Id()); err != nil {
		return nil, err
	}
	ch, err := c.webViewChatOf(in.GetPeer())
	if err != nil {
		return nil, err
	}
	showSettings, err := c.hasSettings(bot.Id())
	if err != nil {
		return nil, err
	}

	if in.GetWriteAllowed() {
		botApp, err := c.botAppOf(app)
		if err != nil {
			return nil, err
		}
		action := mtproto.MakeTLMessageActionBotAllowed(&mtproto.MessageAction{App: botApp}).To_MessageAction()
		if _, err = c.allowWrite(bot.Id(), botstore.WriteAllowedApp, action); err != nil {
			c.Logger.Errorf("messages.requestAppWebView - allow bot %d: %v", bot.Id(), err)
			return nil, err
		}
	}
	if _, err = c.svcCtx.Bots.MarkAppLaunched(c.ctx, c.MD.UserId, app.Id, nowUnix()); err != nil {
		c.Logger.Errorf("messages.requestAppWebView - launch of app %d: %v", app.Id, err)
		return nil, mtproto.ErrInternalServerError
	}

	w := &webView{
		bot:          bot,
		params:       params,
		appURL:       app.URL,
		showSettings: showSettings,
		fullsize:     !in.GetCompact() && !in.GetFullscreen(),
	}
	if ch != nil {
		w.chatType = ch.chatType(bot.Id())
		w.chatInstance = chatInstance(bot.Id(), &pressed{peer: ch.peer}, c.MD.UserId)
		if w.answer, err = c.answerIn(ch); err != nil {
			return nil, err
		}
	}
	return c.open(w)
}
