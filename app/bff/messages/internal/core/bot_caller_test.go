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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// Whether the caller is a bot comes from the session (RpcMetadata.IsBot):
// nothing is looked up for it. A user's keyboard is dropped and a user's
// widget refused without a service being asked (noiseCore has none).
func TestBotCallerFromMetadata(t *testing.T) {
	markup := mtproto.MakeTLReplyKeyboardHide(&mtproto.ReplyMarkup{}).To_ReplyMarkup()
	if got, err := noiseCore(false).botMarkup(markup, false); err != nil || got != nil {
		t.Fatalf("a user's keyboard: %v, %v; want none", got, err)
	}
	if got, err := noiseCore(true).botMarkup(markup, false); err != nil || got == nil {
		t.Fatalf("a bot's keyboard: %v, %v", got, err)
	}

	in := mtproto.MakeTLInputMediaWidgetId(&mtproto.InputMedia{}).To_InputMedia()
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 8}
	if _, err := noiseCore(false).makeWidgetMedia(peer, in); !errors.Is(err, mtproto.ErrInputMediaWidgetNotAllowed) {
		t.Fatalf("a user's widget: %v", err)
	}

	// The bot of a business connection writes the keyboard of the
	// account's message: the request keeps its IsBot.
	c := noiseCore(true)
	c.business = &business.Act{Connection: &business.Connection{UserID: 7, BotID: 9}, PeerID: 8}
	if got, err := c.botMarkup(markup, false); err != nil || got == nil {
		t.Fatalf("a business bot's keyboard: %v, %v", got, err)
	}
}

// A bot sends nothing to a quick reply shortcut: QUICK_REPLIES_BOT_NOT_ALLOWED
// (core.telegram.org/method/messages.sendMessage). The page of
// messages.editMessage names no error for an edit, which stays
// BOT_METHOD_INVALID (quick_reply_edit_test.go).
func TestBotQuickRepliesNotAllowed(t *testing.T) {
	self := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 7}
	shortcut := mtproto.MakeTLInputQuickReplyShortcutId(&mtproto.InputQuickReplyShortcut{ShortcutId: 1}).To_InputQuickReplyShortcut()
	if err := noiseCore(true).checkQuickReplySend(self, shortcut, nil); !errors.Is(err, mtproto.ErrQuickRepliesBotNotAllowed) {
		t.Fatalf("a bot's send to a shortcut: %v", err)
	}
	if err := noiseCore(false).checkQuickReplySend(self, shortcut, nil); err != nil {
		t.Fatalf("a user's send to a shortcut: %v", err)
	}
}
