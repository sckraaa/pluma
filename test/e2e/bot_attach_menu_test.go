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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// attachMenuIcon is the default_static icon of the test's entry.
const attachMenuIcon = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="#2481cc"/></svg>`

func attachMenuBots(ctx context.Context, t *testing.T, u *user, hash int64) *mtproto.AttachMenuBots {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetAttachMenuBots{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAttachMenuBots),
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.getAttachMenuBots: %v", err)
	}
	return o.(interface {
		To_AttachMenuBots() *mtproto.AttachMenuBots
	}).To_AttachMenuBots()
}

// TestAttachMenu: the administrator gives a bot an attachment menu entry
// (task 106, ADR 0068 p. 5). The user sees it inactive, adds it with the
// permission to be written to; the other device gets updateAttachMenuBots
// and the chat with the bot messageActionBotAllowed{attach_menu}; the list
// has the bot with its icon and answers its own hash with
// attachMenuBotsNotModified; after the removal the other device is told
// again and the list is empty.
func TestAttachMenu(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	getState(ctx, t, laptop)

	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	if m := botCreatedRe.FindStringSubmatch(admin(t, "bot", "create", username, "Test Bot", "-owner", phone)); m == nil {
		t.Fatal("pluma-admin bot create printed no bot")
	}
	icon := filepath.Join(t.TempDir(), "icon.svg")
	if err := os.WriteFile(icon, []byte(attachMenuIcon), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := fmt.Sprintf("/tmp/attach-icon-%d.svg", uint64(randomID()))
	stack(t, "cp", icon, "bff:"+dest)
	defer stack(t, "exec", "-T", "bff", "rm", "-f", dest)
	admin(t, "bot", "attach", "set", username, "-name", "Shop", "-url", "https://shop.example/app",
		"-peers", "pm,chat", "-write-access", "on", "-icon", "default_static="+dest)

	peer := resolveBot(ctx, t, alice, username)
	botId := peer.GetUserId()
	bot := &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUser), UserId: botId, AccessHash: peer.GetAccessHash()}

	if r := attachMenuBots(ctx, t, alice, 0); r.GetPredicateName() != mtproto.Predicate_attachMenuBots || len(r.GetBots()) != 0 {
		t.Fatalf("the menu before anything is added: %v", r)
	}

	checkEntry := func(b *mtproto.AttachMenuBot, inactive bool) {
		t.Helper()
		if b.GetBotId() != botId || b.GetShortName() != "Shop" || b.GetInactive() != inactive || !b.GetShowInAttachMenu() ||
			!b.GetRequestWriteAccess() || len(b.GetPeerTypes()) != 2 {
			t.Fatalf("entry %v, want inactive %v", b, inactive)
		}
		if icons := b.GetIcons(); len(icons) != 1 || icons[0].GetName() != "default_static" || icons[0].GetIcon().GetMimeType() != "image/svg+xml" {
			t.Fatalf("icons %v", icons)
		}
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetAttachMenuBot{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getAttachMenuBot),
		Bot:         bot,
	})
	if err != nil {
		t.Fatalf("messages.getAttachMenuBot: %v", err)
	}
	entry := o.(interface {
		To_AttachMenuBotsBot() *mtproto.AttachMenuBotsBot
	}).To_AttachMenuBotsBot()
	checkEntry(entry.GetBot(), true)
	if users := entry.GetUsers(); len(users) != 1 || users[0].GetId() != botId || !users[0].GetBotAttachMenu() {
		t.Fatalf("users of the entry: %v", users)
	}

	toggle := func(enabled, writeAllowed bool) {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesToggleBotInAttachMenu{
			Constructor:  alice.c.Constructor(mtproto.Predicate_messages_toggleBotInAttachMenu),
			WriteAllowed: writeAllowed,
			Bot:          bot,
			Enabled:      mtproto.ToBool(enabled),
		})
		if err != nil {
			t.Fatalf("messages.toggleBotInAttachMenu(%v): %v", enabled, err)
		}
		if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
			t.Fatalf("messages.toggleBotInAttachMenu(%v): %v", enabled, o)
		}
	}
	toggle(true, true)
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateAttachMenuBots, func(*mtproto.Update) bool { return true })
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		a := m.GetAction()
		return m.GetPeerId().GetUserId() == botId && a.GetPredicateName() == mtproto.Predicate_messageActionBotAllowed && a.GetAttachMenu()
	})

	r := attachMenuBots(ctx, t, laptop, 0)
	if r.GetPredicateName() != mtproto.Predicate_attachMenuBots || len(r.GetBots()) != 1 || r.GetHash() == 0 {
		t.Fatalf("the menu with the bot: %v", r)
	}
	checkEntry(r.GetBots()[0], false)
	if users := r.GetUsers(); len(users) != 1 || users[0].GetId() != botId {
		t.Fatalf("users of the menu: %v", users)
	}
	if again := attachMenuBots(ctx, t, laptop, r.GetHash()); again.GetPredicateName() != mtproto.Predicate_attachMenuBotsNotModified {
		t.Errorf("the menu's own hash: %s", again.GetPredicateName())
	}

	toggle(false, false)
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateAttachMenuBots, func(*mtproto.Update) bool { return true })
	if r = attachMenuBots(ctx, t, laptop, r.GetHash()); r.GetPredicateName() != mtproto.Predicate_attachMenuBots || len(r.GetBots()) != 0 || r.GetHash() != 0 {
		t.Fatalf("the menu after the removal: %v", r)
	}
}
