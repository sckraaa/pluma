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

// MessagesRequestMainWebView
// messages.requestMainWebView#c9e01e7b flags:# compact:flags.7?true fullscreen:flags.8?true peer:InputPeer bot:InputUser start_param:flags.1?string theme_params:flags.0?DataJSON platform:string = WebViewResult;
//
// The main mini app of a bot (user.bot_has_main_app): "Open App" of the
// bot's profile and the links t.me/<bot>?startapp (core.telegram.org/api/links),
// opened from the chat the user is in (tdesktop
// inline_bots/bot_attach_web_view.cpp:1320-1346, Android
// ui/bots/BotWebViewAttachedSheet.java:1297-1324). The initData has
// chat_type and chat_instance of that chat (bots/webapps#webappinitdata);
// opened in the chat with the bot, the bot may answer into it (query_id,
// ADR 0068 p. 4). Without compact the app opens full size, the normal mode
// of a direct link (core.telegram.org/api/links, "mode").
func (c *BotsCore) MessagesRequestMainWebView(in *mtproto.TLMessagesRequestMainWebView) (*mtproto.WebViewResult, error) {
	params, err := webViewParamsOf(in.GetPlatform(), in.GetThemeParams(), in.GetStartParam(), in.GetFullscreen())
	if err != nil {
		return nil, err
	}
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	appURL, err := c.svcCtx.Bots.MainApp(c.ctx, bot.Id())
	if err != nil {
		c.Logger.Errorf("messages.requestMainWebView - main app of bot %d: %v", bot.Id(), err)
		return nil, mtproto.ErrInternalServerError
	}
	if appURL == "" {
		return nil, mtproto.ErrBotWebviewDisabled
	}
	if err = checkWebAppURL(appURL); err != nil {
		return nil, err
	}
	ch, err := c.webViewChatOf(in.GetPeer())
	if err != nil {
		return nil, err
	}
	w := &webView{bot: bot, params: params, appURL: appURL, fullsize: !in.GetCompact() && !in.GetFullscreen()}
	if ch != nil {
		w.chatType = ch.chatType(bot.Id())
		w.chatInstance = chatInstance(bot.Id(), &pressed{peer: ch.peer}, c.MD.UserId)
		if ch.peer.IsUser() && ch.peer.PeerId == bot.Id() {
			if w.answer, err = c.answerIn(ch); err != nil {
				return nil, err
			}
		}
	}
	return c.open(w)
}

// answerIn is the answer of a web view opened from the chat for the bot,
// nil when c's user may not write there: the app opens all the same, only
// without a query_id (main and direct apps open from any chat).
func (c *BotsCore) answerIn(ch *webViewChat) (*webViewAnswer, error) {
	if _, err := c.answerFrom(ch, nil); err != nil {
		if refusedAnswer(err) {
			return nil, nil
		}
		return nil, err
	}
	return &webViewAnswer{chat: ch}, nil
}
