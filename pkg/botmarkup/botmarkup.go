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

// Package botmarkup checks the reply markup a bot attaches to a message and
// turns its input buttons into the ones clients receive (task 070; the
// errors are those of core.telegram.org/method/messages.setInlineBotResults
// and messages.sendMessage, the buttons of core.telegram.org/api/bots/buttons).
package botmarkup

import (
	"net/url"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/layercompat"
)

// maxCallbackData: core.telegram.org/bots/api#inlinekeyboardbutton,
// "callback_data 1-64 bytes".
const maxCallbackData = 64

// Env is what Normalize looks up for the buttons that name other accounts.
type Env struct {
	// Self is the bot whose message the markup belongs to: the login bot of
	// a login button that names none (bots/api LoginUrl, bot_username: "If
	// not specified, the current bot's username will be assumed").
	Self int64
	// ResolveUser turns an InputUser of a button into a user id, 0 when it
	// names nobody.
	ResolveUser func(*mtproto.InputUser) int64
	// LoginDomain returns the login domain of the user (bots.login_domain,
	// "" for none); isBot is false when the user is not a bot.
	LoginDomain func(userId int64) (domain string, isBot bool, err error)
}

// Normalize checks markup and returns what is stored on the message: the
// input buttons turned into their received form, requires_password dropped
// (the server asks for no password on a press).
func Normalize(markup *mtproto.ReplyMarkup, env Env) (*mtproto.ReplyMarkup, error) {
	if markup == nil {
		return nil, nil
	}
	// A bot of layer 229 or later sends the buttons of that layer's schema.
	markup = layercompat.Canonical(markup)
	switch markup.GetPredicateName() {
	case mtproto.Predicate_replyKeyboardHide,
		mtproto.Predicate_replyKeyboardForceReply:
		return markup, nil
	case mtproto.Predicate_replyKeyboardMarkup,
		mtproto.Predicate_replyInlineMarkup:
	default:
		return nil, mtproto.ErrReplyMarkupInvalid
	}
	inline := markup.GetPredicateName() == mtproto.Predicate_replyInlineMarkup
	// position numbers the buttons of the markup from 1, row by row: the
	// button_id of a login button, which messages.requestUrlAuth names it
	// by (tdesktop boxes/url_auth_box.cpp:279-295 sends button->buttonId of
	// the button pressed).
	position := int32(0)
	for _, row := range markup.GetRows() {
		for i, button := range row.GetButtons() {
			position++
			normal, err := normalizeButton(button, inline, position, env)
			if err != nil {
				return nil, err
			}
			row.Buttons[i] = normal
		}
	}
	return markup, nil
}

func normalizeButton(b *mtproto.KeyboardButton, inline bool, position int32, env Env) (*mtproto.KeyboardButton, error) {
	if strings.TrimSpace(b.GetText()) == "" {
		return nil, mtproto.ErrButtonTextInvalid
	}
	switch b.GetPredicateName() {
	case mtproto.Predicate_keyboardButtonCallback:
		if !inline {
			return nil, mtproto.ErrButtonTypeInvalid
		}
		if n := len(b.GetData()); n == 0 || n > maxCallbackData {
			return nil, mtproto.ErrButtonDataInvalid
		}
		b.RequiresPassword = false
	case mtproto.Predicate_keyboardButtonUrl:
		if !inline {
			return nil, mtproto.ErrButtonTypeInvalid
		}
		if !strings.Contains(b.GetUrl(), "://") && !strings.HasPrefix(b.GetUrl(), "tg:") {
			return nil, mtproto.ErrButtonUrlInvalid
		}
	case mtproto.Predicate_inputKeyboardButtonRequestPeer:
		// keyboardButtonRequestPeer is what clients get; the fields the
		// answer may carry are the server's to keep and answering is
		// messages.sendBotRequestedPeer's, not served yet.
		return mtproto.MakeTLKeyboardButtonRequestPeer(&mtproto.KeyboardButton{
			Style:       b.GetStyle(),
			Text:        b.GetText(),
			ButtonId:    b.GetButtonId(),
			PeerType:    b.GetPeerType(),
			MaxQuantity: b.GetMaxQuantity(),
		}).To_KeyboardButton(), nil
	case mtproto.Predicate_inputKeyboardButtonUserProfile:
		id := env.ResolveUser(b.GetUserId_INPUTUSER())
		if id == 0 {
			return nil, mtproto.ErrUserIdInvalid
		}
		return mtproto.MakeTLKeyboardButtonUserProfile(&mtproto.KeyboardButton{
			Style:        b.GetStyle(),
			Text:         b.GetText(),
			UserId_INT64: id,
		}).To_KeyboardButton(), nil
	case mtproto.Predicate_inputKeyboardButtonUrlAuth:
		if !inline {
			return nil, mtproto.ErrButtonTypeInvalid
		}
		return loginButton(b, position, env)
	case mtproto.Predicate_keyboardButtonUrlAuth:
		// The received form carries no login bot: a login needs the server
		// to know it (messages.requestUrlAuth), and a plain link in its
		// place would drop the login without telling anybody.
		return nil, mtproto.ErrButtonTypeInvalid
	}
	return b, nil
}

// loginButton turns inputKeyboardButtonUrlAuth into keyboardButtonUrlAuth
// (core.telegram.org/api/url-authorization). The login bot and
// request_write_access stay in the stored button, in UserId_INT64 and
// RequestWriteAccess, for messages.requestUrlAuth: messages are stored as
// JSON of the object, and the TL constructor of keyboardButtonUrlAuth has no
// such fields, so clients never get them. The host of the link must be the
// login domain of the bot (bots/api LoginUrl: "The url's domain must be the
// same as the domain linked with the bot"), BOT_DOMAIN_INVALID otherwise.
func loginButton(b *mtproto.KeyboardButton, position int32, env Env) (*mtproto.KeyboardButton, error) {
	host, ok := LoginHost(b.GetUrl())
	if !ok {
		return nil, mtproto.ErrButtonUrlInvalid
	}
	botId := env.Self
	if in := b.GetBot(); in != nil && in.GetPredicateName() != mtproto.Predicate_inputUserEmpty {
		if botId = env.ResolveUser(in); botId == 0 {
			return nil, mtproto.ErrBotInvalid
		}
	}
	if botId == 0 || env.LoginDomain == nil {
		return nil, mtproto.ErrBotInvalid
	}
	domain, isBot, err := env.LoginDomain(botId)
	if err != nil {
		return nil, err
	}
	if !isBot {
		return nil, mtproto.ErrBotInvalid
	}
	if domain == "" || domain != host {
		return nil, mtproto.ErrBotDomainInvalid
	}
	return mtproto.MakeTLKeyboardButtonUrlAuth(&mtproto.KeyboardButton{
		Style:              b.GetStyle(),
		Text:               b.GetText(),
		FwdText:            b.GetFwdText(),
		Url:                b.GetUrl(),
		ButtonId:           position,
		UserId_INT64:       botId,
		RequestWriteAccess: b.GetRequestWriteAccess(),
	}).To_KeyboardButton(), nil
}

// LoginHost returns the host of an https link in lower case, without a
// port: what is compared with the login domain of a bot. ok is false for
// anything else: the address gets the signed data of the user in its query,
// which plain http would show to everyone on the way. That bots/api
// LoginUrl asks for an HTTPS address is recalled, not verified.
func LoginHost(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil {
		return "", false
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), true
}
