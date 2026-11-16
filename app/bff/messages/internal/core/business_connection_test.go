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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// A connected business bot writing the account's code into a chat of the
// account (task 105) invalidates it, as the account's own message does:
// the request runs as the account, with the bot's IsBot.
func TestBusinessBotLoginCodeIsInvalidated(t *testing.T) {
	e := newCodesEnv()
	c := e.core(true)
	c.business = &business.Act{Connection: &business.Connection{UserID: codesUserId, BotID: 9}, PeerID: 8}
	c.invalidateSentLoginCodes("Login code: 123456")
	if len(e.codes.calls) != 1 || !slices.Equal(e.codes.calls[0], []string{"123456"}) || e.codes.phone != codesPhone {
		t.Fatalf("invalidations %q of phone %q; want [123456] of the account's phone", e.codes.calls, e.codes.phone)
	}
}

// A connected business bot sends as its account, and a user's message
// carries no widget (task 106): INPUT_MEDIA_WIDGET_NOT_ALLOWED before a
// widget is made.
func TestBusinessBotWidgetNotAllowed(t *testing.T) {
	e := newCodesEnv()
	c := e.core(true)
	c.business = &business.Act{Connection: &business.Connection{UserID: codesUserId, BotID: 9}, PeerID: 8}
	in := mtproto.MakeTLInputMediaWidgetId(&mtproto.InputMedia{}).To_InputMedia()
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 8}
	if _, err := c.makeWidgetMedia(peer, in); !errors.Is(err, mtproto.ErrInputMediaWidgetNotAllowed) {
		t.Fatalf("a widget through a business connection: %v", err)
	}
}

// Nor does a connected business bot send a game for its account: the
// press of its message could not start it.
func TestBusinessBotGameNotAllowed(t *testing.T) {
	e := newCodesEnv()
	c := e.core(true)
	c.business = &business.Act{Connection: &business.Connection{UserID: codesUserId, BotID: 9}, PeerID: 8}
	in := mtproto.MakeTLInputMediaGame(&mtproto.InputMedia{}).To_InputMedia()
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 8}
	if _, err := c.makeGameMedia(peer, in); !errors.Is(err, mtproto.ErrBotAccessForbidden) {
		t.Fatalf("a game through a business connection: %v", err)
	}
}
