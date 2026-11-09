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
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/layercompat"
)

// maxCallbackData: core.telegram.org/bots/api#inlinekeyboardbutton,
// "callback_data 1-64 bytes".
const maxCallbackData = 64

// Normalize checks markup and returns what is stored on the message: the
// input buttons turned into their received form, requires_password dropped
// (the server asks for no password on a press). resolve turns an InputUser of a button into a user id, 0
// when it names nobody.
func Normalize(markup *mtproto.ReplyMarkup, resolve func(*mtproto.InputUser) int64) (*mtproto.ReplyMarkup, error) {
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
	for _, row := range markup.GetRows() {
		for i, button := range row.GetButtons() {
			normal, err := normalizeButton(button, inline, resolve)
			if err != nil {
				return nil, err
			}
			row.Buttons[i] = normal
		}
	}
	return markup, nil
}

func normalizeButton(b *mtproto.KeyboardButton, inline bool, resolve func(*mtproto.InputUser) int64) (*mtproto.KeyboardButton, error) {
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
		id := resolve(b.GetUserId_INPUTUSER())
		if id == 0 {
			return nil, mtproto.ErrUserIdInvalid
		}
		return mtproto.MakeTLKeyboardButtonUserProfile(&mtproto.KeyboardButton{
			Style:        b.GetStyle(),
			Text:         b.GetText(),
			UserId_INT64: id,
		}).To_KeyboardButton(), nil
	case mtproto.Predicate_inputKeyboardButtonUrlAuth,
		mtproto.Predicate_keyboardButtonUrlAuth:
		// A login button needs the server to take part
		// (messages.requestUrlAuth); a plain link in its place would drop
		// the login without telling anybody.
		return nil, mtproto.ErrButtonTypeInvalid
	}
	return b, nil
}
