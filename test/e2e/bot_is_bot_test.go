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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// TestBotIsBot: the session marks the requests of a bot's key
// (RpcMetadata.IsBot, task 108). A bot logged in with
// auth.importBotAuthorization gets BOT_METHOD_INVALID for methods only users
// may call, one of each area (core.telegram.org: "Only users can use this
// method"), and QUICK_REPLIES_BOT_NOT_ALLOWED for a send to a quick reply
// shortcut (core.telegram.org/method/messages.sendMessage); the same
// requests of a user are not refused so. A bot's keyboard reaches the
// user, a user's keyboard is dropped.
func TestBotIsBot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	// Layer 228, whose keyboards the bot builds below.
	bot := botLoggedIn(ctx, t, phone, username)
	getState(ctx, t, bot)

	toBot := resolveBot(ctx, t, alice, username)
	toAlice := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}

	usersOnly := func(u *user) map[string]mtproto.TLObject {
		self := &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}
		peer := toBot
		if u == bot {
			peer = toAlice
		}
		return map[string]mtproto.TLObject{
			// Translations.
			"messages.translateText": &mtproto.TLMessagesTranslateText{
				Constructor:                     u.c.Constructor(mtproto.Predicate_messages_translateText),
				Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{plainText(u, "hello")},
				ToLang:                          "de",
			},
			// Games.
			"messages.getEmojiGameInfo": &mtproto.TLMessagesGetEmojiGameInfo{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_getEmojiGameInfo),
			},
			// Quick replies.
			"messages.getQuickReplies": &mtproto.TLMessagesGetQuickReplies{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_getQuickReplies),
			},
			// Profiles and chats.
			"messages.getPeerSettings": &mtproto.TLMessagesGetPeerSettings{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_getPeerSettings),
				Peer:        peer,
			},
			// Stories.
			"stories.getChatsToSend": &mtproto.TLStoriesGetChatsToSend{
				Constructor: u.c.Constructor(mtproto.Predicate_stories_getChatsToSend),
			},
			// Stickers.
			"messages.getRecentStickers": &mtproto.TLMessagesGetRecentStickers{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_getRecentStickers),
			},
			// Search.
			"messages.search": &mtproto.TLMessagesSearch{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_search),
				Peer:        self,
				Q:           "x",
				Filter:      &mtproto.MessagesFilter{Constructor: u.c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)},
				Limit:       10,
			},
		}
	}
	for name, req := range usersOnly(bot) {
		if _, err := bot.c.Invoke(ctx, req); !strings.Contains(fmt.Sprint(err), "BOT_METHOD_INVALID") {
			t.Errorf("%s of a bot: %v, want BOT_METHOD_INVALID", name, err)
		}
	}
	for name, req := range usersOnly(alice) {
		if _, err := alice.c.Invoke(ctx, req); strings.Contains(fmt.Sprint(err), "BOT_METHOD_INVALID") {
			t.Errorf("%s of a user: %v", name, err)
		}
	}

	// Inline mode is the users': a bot asks no bot.
	if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesGetInlineBotResults{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_getInlineBotResults),
		Bot:         &mtproto.InputUser{Constructor: bot.c.Constructor(mtproto.Predicate_inputUserSelf)},
		Peer:        toAlice,
		Query:       "q",
	}); !strings.Contains(fmt.Sprint(err), "BOT_METHOD_INVALID") {
		t.Errorf("messages.getInlineBotResults of a bot: %v", err)
	}

	// A bot keeps no quick replies.
	if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor:        bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:               &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Message:            "later",
		RandomId:           randomID(),
		QuickReplyShortcut: &mtproto.InputQuickReplyShortcut{Constructor: bot.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: "later"},
	}); !strings.Contains(fmt.Sprint(err), "QUICK_REPLIES_BOT_NOT_ALLOWED") {
		t.Errorf("a bot's send to a quick reply shortcut: %v", err)
	}

	// A keyboard: a user's is dropped, a bot's reaches the user.
	userMarkup := &mtproto.ReplyMarkup{Constructor: alice.c.Constructor(mtproto.Predicate_replyKeyboardHide)}
	botMarkup := &mtproto.ReplyMarkup{
		Constructor: bot.c.Constructor(mtproto.Predicate_replyInlineMarkup),
		Rows: []*mtproto.KeyboardButtonRow{{
			Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonRow),
			Buttons: []*mtproto.KeyboardButton{{
				Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonCallback),
				Text:        "Go", Data: []byte("go"),
			}},
		}},
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toBot, Message: "from alice", RandomId: randomID(), ReplyMarkup: userMarkup,
	}); err != nil {
		t.Fatalf("messages.sendMessage from the user: %v", err)
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "from alice" {
			return false
		}
		if m.GetReplyMarkup() != nil {
			t.Errorf("a user's message carries a keyboard: %v", m.GetReplyMarkup())
		}
		return true
	})
	if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toAlice, Message: "from the bot", RandomId: randomID(), ReplyMarkup: botMarkup,
	}); err != nil {
		t.Fatalf("messages.sendMessage from the bot: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetMessage() == "from the bot" && m.GetReplyMarkup().GetPredicateName() == mtproto.Predicate_replyInlineMarkup
	})
}
