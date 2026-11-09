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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

func setChatWallPaper(ctx context.Context, u *user, req *mtproto.TLMessagesSetChatWallPaper) (*mtproto.Updates, error) {
	req.Constructor = u.c.Constructor(mtproto.Predicate_messages_setChatWallPaper)
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

// A fill wallpaper needs no file: the client names no document, only settings
// (core.telegram.org/api/wallpapers).
func fillWallpaper(u *user) (*mtproto.InputWallPaper, *mtproto.WallPaperSettings) {
	return &mtproto.InputWallPaper{Constructor: u.c.Constructor(mtproto.Predicate_inputWallPaperNoFile)},
		&mtproto.WallPaperSettings{
			Constructor:     u.c.Constructor(mtproto.Predicate_wallPaperSettings),
			BackgroundColor: mtproto.MakeFlagsInt32(0x1a2b3c),
		}
}

func serviceAction(ups *mtproto.Updates) *mtproto.MessageAction {
	for _, u := range ups.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			return u.GetMessage_MESSAGE().GetAction()
		}
	}
	return nil
}

// A fill wallpaper in a private chat (task 065, core.telegram.org/api/wallpapers):
// the chat gets a service message for both, the side that set it has it in
// userFull and is told of it, the other side is not changed and can accept
// it with the message id (same), the setter's Premium decides for_both;
// a reset clears only the own side.
func TestChatWallpaperPrivate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	toBob := inputPeerOf(ctx, t, alice, bob)
	toAlice := inputPeerOf(ctx, t, bob, alice)
	inputBob := &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUser), UserId: bob.self.GetId(), AccessHash: bob.self.GetAccessHash().GetValue()}
	inputAlice := &mtproto.InputUser{Constructor: bob.c.Constructor(mtproto.Predicate_inputUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}
	wall, settings := fillWallpaper(alice)

	// Premium for both is refused for a user without it.
	if _, err := setChatWallPaper(ctx, alice, &mtproto.TLMessagesSetChatWallPaper{ForBoth: true, Peer: toBob, Wallpaper: wall, Settings: settings}); !rpcErrorIs(err, "PREMIUM_ACCOUNT_REQUIRED") {
		t.Fatalf("for_both without Premium: %v", err)
	}
	// A revert with nothing overridden has nothing to bring back.
	if _, err := setChatWallPaper(ctx, alice, &mtproto.TLMessagesSetChatWallPaper{Revert: true, Peer: toBob}); !rpcErrorIs(err, "WALLPAPER_NOT_FOUND") {
		t.Fatalf("revert: %v", err)
	}

	ups, err := setChatWallPaper(ctx, alice, &mtproto.TLMessagesSetChatWallPaper{Peer: toBob, Wallpaper: wall, Settings: settings})
	if err != nil {
		t.Fatalf("setChatWallPaper: %v", err)
	}
	action := serviceAction(ups)
	if action.GetPredicateName() != mtproto.Predicate_messageActionSetChatWallPaper || action.GetWallpaper().GetSettings().GetBackgroundColor().GetValue() != 0x1a2b3c {
		t.Fatalf("the service message: %v", ups)
	}
	if full, _ := fullUser(ctx, t, alice, inputBob); full.GetWallpaper() == nil || full.GetWallpaperOverridden() {
		t.Fatalf("Alice's userFull: %v", full)
	}
	if full, _ := fullUser(ctx, t, bob, inputAlice); full.GetWallpaper() != nil {
		t.Fatalf("Bob did not choose it: %v", full)
	}

	// Bob sees the message and accepts with its id: both sides keep it.
	var msgID int32
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		if u.GetMessage_MESSAGE().GetAction().GetPredicateName() != mtproto.Predicate_messageActionSetChatWallPaper {
			return false
		}
		msgID = u.GetMessage_MESSAGE().GetId()
		return true
	})
	got, err := setChatWallPaper(ctx, bob, &mtproto.TLMessagesSetChatWallPaper{Peer: toAlice, Settings: settings, Id: mtproto.MakeFlagsInt32(msgID)})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if a := serviceAction(got); a.GetPredicateName() != mtproto.Predicate_messageActionSetChatWallPaper || !a.GetSame() {
		t.Fatalf("same: %v", got)
	}
	if full, _ := fullUser(ctx, t, bob, inputAlice); full.GetWallpaper() == nil {
		t.Fatalf("Bob's userFull: %v", full)
	}

	// Alice clears her side only.
	if _, err = setChatWallPaper(ctx, alice, &mtproto.TLMessagesSetChatWallPaper{Peer: toBob}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if full, _ := fullUser(ctx, t, alice, inputBob); full.GetWallpaper() != nil {
		t.Fatalf("after clear: %v", full)
	}
	if full, _ := fullUser(ctx, t, bob, inputAlice); full.GetWallpaper() == nil {
		t.Fatalf("Bob keeps his: %v", full)
	}
}

func channelFullOf(ctx context.Context, t *testing.T, u *user, ch *mtproto.Chat) *mtproto.ChatFull {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()},
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	return o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat()
}

// A channel's wallpaper belongs to those allowed to change its info
// (task 065): the admin sets a fill one, the channelFull shows it, the
// members are told by updatePeerWallpaper, and the owner can clear it.
func TestChatWallpaperChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 1)
	alice := login(ctx, t, phones[0], "Alice")
	getState(ctx, t, alice)
	_, ch := createChannel(ctx, t, alice, "wallpapers", false)
	peer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}

	wall, settings := fillWallpaper(alice)
	if _, err := setChatWallPaper(ctx, alice, &mtproto.TLMessagesSetChatWallPaper{Peer: peer, Wallpaper: wall, Settings: settings}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if full := channelFullOf(ctx, t, alice, ch); full.GetWallpaper() == nil {
		t.Fatalf("channelFull: %v", full)
	}
	if _, err := setChatWallPaper(ctx, alice, &mtproto.TLMessagesSetChatWallPaper{Peer: peer}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if full := channelFullOf(ctx, t, alice, ch); full.GetWallpaper() != nil {
		t.Fatalf("after clear: %v", full)
	}
}

func invokeTheme(ctx context.Context, t *testing.T, c *mtclient.Client, req mtproto.TLObject) *mtproto.Theme {
	t.Helper()
	o, err := c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	return o.(interface{ To_Theme() *mtproto.Theme }).To_Theme()
}

// A cloud theme (task 065, core.telegram.org/api/themes): the creator makes it
// with a slug and settings, somebody else finds it by slug and installs it,
// only the creator may update it, the installer is told by updateTheme.
func TestCloudTheme(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	slug := "cloud_theme_" + phones[0][len(phones[0])-6:]

	settings := []*mtproto.InputThemeSettings{{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputThemeSettings),
		BaseTheme:   mtproto.MakeTLBaseThemeDay(nil).To_BaseTheme(),
		AccentColor: 0xff0000,
	}}
	created := invokeTheme(ctx, t, alice.c, &mtproto.TLAccountCreateTheme{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_createTheme),
		Slug:        slug, Title: "Theme", Settings: settings,
	})
	if created.GetSlug() != slug || !created.GetCreator() {
		t.Fatalf("created: %v", created)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountCreateTheme{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_createTheme),
		Slug:        slug, Title: "",
	}); !rpcErrorIs(err, "THEME_SLUG_OCCUPIED") {
		t.Fatalf("a taken slug is reported before the title: %v", err)
	}

	bySlug := &mtproto.InputTheme{Constructor: bob.c.Constructor(mtproto.Predicate_inputThemeSlug), Slug: slug}
	seen := invokeTheme(ctx, t, bob.c, &mtproto.TLAccountGetTheme{Constructor: bob.c.Constructor(mtproto.Predicate_account_getTheme), Format: "android", Theme: bySlug})
	if seen.GetId() != created.GetId() || seen.GetCreator() {
		t.Fatalf("Bob's view: %v", seen)
	}
	byId := &mtproto.InputTheme{Constructor: bob.c.Constructor(mtproto.Predicate_inputTheme), Id: seen.GetId(), AccessHash: seen.GetAccessHash()}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLAccountInstallTheme{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_installTheme),
		Format:      mtproto.MakeFlagsString("android"), Theme: byId,
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLAccountUpdateTheme{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_updateTheme), Format: "android", Theme: byId, Title: mtproto.MakeFlagsString("Stolen"),
	}); !rpcErrorIs(err, "THEME_INVALID") {
		t.Fatalf("not the creator: %v", err)
	}

	updated := invokeTheme(ctx, t, alice.c, &mtproto.TLAccountUpdateTheme{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateTheme),
		Format:      "android",
		Theme:       &mtproto.InputTheme{Constructor: alice.c.Constructor(mtproto.Predicate_inputTheme), Id: seen.GetId(), AccessHash: seen.GetAccessHash()},
		Title:       mtproto.MakeFlagsString("Renamed"),
	})
	if updated.GetTitle() != "Renamed" || updated.GetInstallsCount().GetValue() != 1 {
		t.Fatalf("updated: %v", updated)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateTheme, func(u *mtproto.Update) bool {
		return u.GetTheme().GetTitle() == "Renamed"
	})
}
