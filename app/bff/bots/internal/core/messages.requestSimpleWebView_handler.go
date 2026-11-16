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

// MessagesRequestSimpleWebView413A3E73
// messages.requestSimpleWebView#413a3e73 flags:# from_switch_webview:flags.1?true from_side_menu:flags.2?true compact:flags.7?true fullscreen:flags.8?true bot:InputUser url:flags.3?string start_param:flags.4?string theme_params:flags.0?DataJSON platform:string = WebViewResult;
//
// A mini app with no chat to answer into (core.telegram.org/api/bots/webapps):
// the web_app button of a reply keyboard (url), the switch_webview button
// of inline results (from_switch_webview with its url) and the side menu
// entry of an attachment menu bot (from_side_menu), as tdesktop sends them
// (inline_bots/bot_attach_web_view.cpp:1291-1318). No query_id: the app
// answers with sendData (messages.sendWebViewData) or switchInlineQuery.
func (c *BotsCore) MessagesRequestSimpleWebView413A3E73(in *mtproto.TLMessagesRequestSimpleWebView413A3E73) (*mtproto.WebViewResult, error) {
	params, err := webViewParamsOf(in.GetPlatform(), in.GetThemeParams(), in.GetStartParam(), in.GetFullscreen())
	if err != nil {
		return nil, err
	}
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	w := &webView{bot: bot, params: params, appURL: in.GetUrl().GetValue()}
	switch {
	case in.GetFromSideMenu():
		m, err := c.attachMenuOf(bot.Id())
		if err != nil {
			return nil, err
		}
		if !m.ShowInSideMenu {
			return nil, mtproto.ErrBotWebviewDisabled
		}
		w.appURL, w.showSettings = m.URL, m.HasSettings
	case in.GetFromSwitchWebview():
		// Only the button of the inline results the user got from the bot
		// (dao.RememberSwitchWebview); the page may switch to inline mode
		// (tgWebAppBotInline).
		shown, err := c.svcCtx.SwitchWebview(c.ctx, bot.Id(), c.MD.UserId)
		if err != nil {
			c.Logger.Errorf("messages.requestSimpleWebView - switch_webview of bot %d: %v", bot.Id(), err)
			return nil, mtproto.ErrInternalServerError
		}
		if shown == "" || shown != w.appURL {
			return nil, mtproto.ErrUrlInvalid
		}
		w.botInline = true
	}
	if err = checkWebAppURL(w.appURL); err != nil {
		return nil, err
	}
	return c.open(w)
}
