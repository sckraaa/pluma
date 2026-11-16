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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"

	"github.com/zeromicro/go-zero/core/jsonx"
)

func TestPreparedPeerKinds(t *testing.T) {
	got, err := preparedPeerKinds(nil)
	if err != nil || !slices.Equal(got, defaultPeerKinds) {
		t.Fatalf("default: %v %v", got, err)
	}
	got, err = preparedPeerKinds([]*mtproto.InlineQueryPeerType{
		mtproto.MakeTLInlineQueryPeerTypeChat(nil).To_InlineQueryPeerType(),
		mtproto.MakeTLInlineQueryPeerTypeBroadcast(nil).To_InlineQueryPeerType(),
		mtproto.MakeTLInlineQueryPeerTypeChat(nil).To_InlineQueryPeerType(),
	})
	if err != nil || !slices.Equal(got, []string{"chat", "broadcast"}) {
		t.Fatalf("listed: %v %v", got, err)
	}
	if _, err = preparedPeerKinds([]*mtproto.InlineQueryPeerType{{}}); !errors.Is(err, mtproto.ErrPeerTypesInvalid) {
		t.Fatalf("unknown type: %v", err)
	}

	// The kinds go back to the clients as the same constructors.
	all := []string{"same_bot", "pm", "bot", "chat", "megagroup", "broadcast"}
	tl := peerTypesTL(all)
	back, err := preparedPeerKinds(tl)
	if err != nil || !slices.Equal(back, all) {
		t.Fatalf("round trip: %v %v", back, err)
	}
	for _, p := range tl {
		x := mtproto.NewEncodeBuf(8)
		if err := p.Encode(x, 233); err != nil {
			t.Fatalf("%s: %v", p.GetPredicateName(), err)
		}
	}
}

func TestPeerKindAllowed(t *testing.T) {
	kinds := []string{"pm", "bot"}
	for kind, want := range map[string]bool{"pm": true, "bot": true, "same_bot": true, "chat": false, "megagroup": false, "broadcast": false} {
		if peerKindAllowed(kinds, kind) != want {
			t.Errorf("%s: want %v", kind, want)
		}
	}
	if peerKindAllowed([]string{"chat"}, "same_bot") {
		t.Error("the chat with the bot is one of the bots' chats")
	}
}

// A prepared message keeps its result and game through the JSON of
// bot_prepared.
func TestPreparedMessagePayload(t *testing.T) {
	m := preparedMessage{
		Result: mtproto.MakeTLBotInlineResult(&mtproto.BotInlineResult{
			Id: "1", Type: "article",
			SendMessage: mtproto.MakeTLBotInlineMessageText(&mtproto.BotInlineMessage{Message: "hi"}).To_BotInlineMessage(),
		}).To_BotInlineResult(),
		Game:      &dao.GameRef{Id: 5, AccessHash: 6},
		PeerTypes: []string{"pm"},
	}
	b, err := jsonx.Marshal(&m)
	if err != nil {
		t.Fatal(err)
	}
	var back preparedMessage
	if err = jsonx.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Result.GetPredicateName() != mtproto.Predicate_botInlineResult || back.Result.GetSendMessage().GetMessage() != "hi" ||
		*back.Game != *m.Game || !slices.Equal(back.PeerTypes, m.PeerTypes) {
		t.Fatalf("got %+v", back)
	}
}

func TestWellFormedPreparedId(t *testing.T) {
	for id, want := range map[string]bool{
		"AAAAAAAAAAAAAAAAAAAAAAAA":  true,
		"AAAAAAAAAAAAAAAAAAAAAAA":   false,
		"AAAAAAAAAAAAAAAAAAAAAAA=":  false,
		"AAAAAAAAAAAAAAAAAAAAAAAAA": false,
		"":                          false,
		"AAAAAAAAAAAAAAAAAAAAAA+/":  false,
		"AAAAAAAAAAAAAAAAAAAAAA-_":  true,
	} {
		if wellFormedPreparedId(id) != want {
			t.Errorf("%q: want %v", id, want)
		}
	}
}

func TestRequestedPeerList(t *testing.T) {
	user := func(id int64) *mtproto.InputPeer {
		return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id}).To_InputPeer()
	}
	if _, err := requestedPeerList(nil, 1, 1); !errors.Is(err, mtproto.ErrPeersListEmpty) {
		t.Errorf("none: %v", err)
	}
	if _, err := requestedPeerList([]*mtproto.InputPeer{user(2), user(3)}, 1, 1); !errors.Is(err, mtproto.ErrLimitInvalid) {
		t.Errorf("over max_quantity: %v", err)
	}
	// max_quantity 0 asks for one; past 10 for ten.
	if _, err := requestedPeerList([]*mtproto.InputPeer{user(2)}, 1, 0); err != nil {
		t.Errorf("one of 0: %v", err)
	}
	many := make([]*mtproto.InputPeer, 11)
	for i := range many {
		many[i] = user(int64(i + 2))
	}
	if _, err := requestedPeerList(many, 1, 50); !errors.Is(err, mtproto.ErrLimitInvalid) {
		t.Errorf("11 of 50: %v", err)
	}
	if _, err := requestedPeerList([]*mtproto.InputPeer{user(2), user(2)}, 1, 2); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("twice: %v", err)
	}
	self := mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
	got, err := requestedPeerList([]*mtproto.InputPeer{self, user(3)}, 1, 2)
	if err != nil || !got[0].IsUser() || got[0].PeerId != 1 || got[1].PeerId != 3 {
		t.Errorf("self and another: %v %v", got, err)
	}
}

func TestFindRequestPeerButton(t *testing.T) {
	markup := mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
		mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
			mtproto.MakeTLKeyboardButton(&mtproto.KeyboardButton{Text: "Plain"}).To_KeyboardButton(),
			mtproto.MakeTLKeyboardButtonRequestPeer(&mtproto.KeyboardButton{Text: "Pick", ButtonId: 7}).To_KeyboardButton(),
		}}).To_KeyboardButtonRow(),
	}}).To_ReplyMarkup()
	if b := findRequestPeerButton(markup, 7); b == nil || b.GetText() != "Pick" {
		t.Errorf("button 7: %v", b)
	}
	if b := findRequestPeerButton(markup, 8); b != nil {
		t.Errorf("button 8: %v", b)
	}
	if b := findRequestPeerButton(nil, 7); b != nil {
		t.Errorf("no markup: %v", b)
	}
}

func TestRightsChecks(t *testing.T) {
	want := mtproto.MakeTLChatAdminRights(&mtproto.ChatAdminRights{DeleteMessages: true, PinMessages: true}).To_ChatAdminRights()
	if !holds(basicAdminRights, want) || holds(mtproto.ADMIN_DELETE_MESSAGES, want) || !holds(0, nil) {
		t.Error("holds")
	}
	if !boolMatches(nil, true) || !boolMatches(mtproto.BoolTrue, true) || boolMatches(mtproto.BoolFalse, true) {
		t.Error("boolMatches")
	}
}

func TestMergeUpdates(t *testing.T) {
	a := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: 9}).To_Update()},
		Chats:   []*mtproto.Chat{mtproto.MakeTLChannel(&mtproto.Chat{Id: 9}).To_Chat()},
		Date:    10,
	}).To_Updates()
	b := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateMessageID(&mtproto.Update{RandomId: 1}).To_Update()},
		Chats:   []*mtproto.Chat{mtproto.MakeTLChannel(&mtproto.Chat{Id: 9}).To_Chat()},
		Date:    12,
	}).To_Updates()
	m := mergeUpdates(a, b)
	if len(m.GetUpdates()) != 2 || m.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannel ||
		len(m.GetChats()) != 1 || m.GetDate() != 12 {
		t.Fatalf("got %v", m)
	}
}
