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

// MessagesRequestWebView
// messages.requestWebView#269dc2c1 flags:# from_bot_menu:flags.4?true silent:flags.5?true compact:flags.7?true fullscreen:flags.8?true peer:InputPeer bot:InputUser url:flags.1?string start_param:flags.3?string theme_params:flags.2?DataJSON platform:string reply_to:flags.0?InputReplyTo send_as:flags.13?InputPeer = WebViewResult;
//
// A mini app the bot may answer into the chat (core.telegram.org/api/bots/webapps):
// the web_app button of an inline keyboard (url) and the menu button
// (from_bot_menu with url: tdesktop inline_bots/bot_attach_web_view.cpp:1249-1289;
// Android sends the menu button's url without from_bot_menu,
// ui/bots/BotWebViewAttachedSheet.java:1161-1171), both only in the chat
// with the bot (method/messages.requestWebView: BOT_WEBVIEW_DISABLED), and
// the attachment menu entry of the bot (no url), in a chat of the entry's
// peer types. The answer gets a query_id, which the client prolongs
// (messages.prolongWebView) and the bot answers with
// messages.sendWebViewResultMessage.
func (c *BotsCore) MessagesRequestWebView(in *mtproto.TLMessagesRequestWebView) (*mtproto.WebViewResult, error) {
	params, err := webViewParamsOf(in.GetPlatform(), in.GetThemeParams(), in.GetStartParam(), in.GetFullscreen())
	if err != nil {
		return nil, err
	}
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	ch, err := c.webViewChatOf(in.GetPeer())
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	w := &webView{bot: bot, params: params}

	if in.GetFromBotMenu() || in.GetUrl() != nil {
		if !ch.peer.IsUser() || ch.peer.PeerId != bot.Id() {
			return nil, mtproto.ErrBotWebviewDisabled
		}
		w.appURL = in.GetUrl().GetValue()
		if w.appURL == "" {
			// The menu button without its address (Android sets the flag of
			// url whatever it has, BotWebViewAttachedSheet.java:1169-1170):
			// the one the user has.
			button, err := c.svcCtx.Bots.MenuButton(c.ctx, bot.Id(), c.MD.UserId)
			if err != nil {
				c.Logger.Errorf("messages.requestWebView - menu button of bot %d: %v", bot.Id(), err)
				return nil, mtproto.ErrInternalServerError
			}
			if button.GetPredicateName() != mtproto.Predicate_botMenuButton {
				return nil, mtproto.ErrBotWebviewDisabled
			}
			w.appURL = button.GetUrl()
		}
	} else {
		m, err := c.attachMenuOf(bot.Id())
		if err != nil {
			return nil, err
		}
		if !m.ShowInAttachMenu || m.PeerTypes&ch.attachPeerBit(bot.Id()) == 0 {
			return nil, mtproto.ErrBotWebviewDisabled
		}
		w.appURL, w.showSettings = m.URL, m.HasSettings
		// receiver for a private chat, chat for a group or channel
		// (core.telegram.org/bots/webapps#webappinitdata: both only for
		// the attachment menu).
		if ch.user != nil {
			w.receiver = ch.user
		} else {
			w.chat = c.webAppChat(ch, bot.Id())
		}
	}
	if err = checkWebAppURL(w.appURL); err != nil {
		return nil, err
	}
	if _, err = c.answerFrom(ch, in.GetSendAs()); err != nil {
		return nil, err
	}
	w.answer = &webViewAnswer{chat: ch, replyTo: in.GetReplyTo(), silent: in.GetSilent(), sendAs: in.GetSendAs()}
	return c.open(w)
}
