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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// roundTrip encodes the bot at layer 233 and decodes it as a client would.
func roundTrip(t *testing.T, bot *mtproto.AttachMenuBot) *mtproto.AttachMenuBot {
	t.Helper()
	x := mtproto.NewEncodeBuf(512)
	if err := bot.Encode(x, 233); err != nil {
		t.Fatalf("encode: %v", err)
	}
	d := mtproto.NewDecodeBuf(x.GetBuf())
	o := d.Object()
	if err := d.GetError(); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, ok := o.(*mtproto.TLAttachMenuBot)
	if !ok {
		t.Fatalf("decoded %T, want attachMenuBot", o)
	}
	return got.To_AttachMenuBot()
}

func peerTypeNames(types []*mtproto.AttachMenuPeerType) []string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, t.GetPredicateName())
	}
	return names
}

// An entry becomes attachMenuBot as the clients read it: the flags of the
// entry, the peer types in the order of the schema, the icons with their
// documents and colors, an icon without its document left out and named.
func TestAttachMenuBotOf(t *testing.T) {
	m := &botstore.AttachMenu{
		BotId: 42, ShortName: "Shop", URL: "https://shop.example/app",
		ShowInAttachMenu: true, ShowInSideMenu: true, RequestWriteAccess: true, HasSettings: true,
		PeerTypes: botstore.AttachPeerBroadcast | botstore.AttachPeerPM | botstore.AttachPeerSameBotPM,
		Icons: []botstore.AttachIcon{
			{Name: "default_static", DocumentId: 501, Colors: []botstore.AttachIconColor{{Name: "light_icon", Color: 0x112233}, {Name: "dark_icon", Color: 0xddeeff}}},
			{Name: "android_animated", DocumentId: 502},
			{Name: "ios_static", DocumentId: 503},
		},
	}
	docs := map[int64]*mtproto.Document{
		501: mtproto.MakeTLDocument(&mtproto.Document{Id: 501, AccessHash: 7, FileReference: []byte{}, MimeType: "image/svg+xml", Size2: 120, DcId: 1, Attributes: []*mtproto.DocumentAttribute{}}).To_Document(),
		503: mtproto.MakeTLDocument(&mtproto.Document{Id: 503, AccessHash: 8, FileReference: []byte{}, MimeType: "image/svg+xml", Size2: 90, DcId: 1, Attributes: []*mtproto.DocumentAttribute{}}).To_Document(),
	}
	bot, missing := attachMenuBotOf(m, docs, true)
	if !slices.Equal(missing, []string{"android_animated"}) {
		t.Errorf("missing icons %v, want android_animated", missing)
	}
	got := roundTrip(t, bot)
	if got.GetBotId() != 42 || got.GetShortName() != "Shop" || !got.GetInactive() || !got.GetShowInAttachMenu() || !got.GetShowInSideMenu() ||
		!got.GetRequestWriteAccess() || !got.GetHasSettings() || got.GetSideMenuDisclaimerNeeded() {
		t.Errorf("flags of %v", got)
	}
	want := []string{mtproto.Predicate_attachMenuPeerTypeSameBotPM, mtproto.Predicate_attachMenuPeerTypePM, mtproto.Predicate_attachMenuPeerTypeBroadcast}
	if names := peerTypeNames(got.GetPeerTypes()); !slices.Equal(names, want) {
		t.Errorf("peer types %v, want %v", names, want)
	}
	icons := got.GetIcons()
	if len(icons) != 2 || icons[0].GetName() != "default_static" || icons[0].GetIcon().GetId() != 501 || icons[1].GetName() != "ios_static" || icons[1].GetIcon().GetId() != 503 {
		t.Fatalf("icons %v", icons)
	}
	colors := icons[0].GetColors()
	if len(colors) != 2 || colors[0].GetName() != "light_icon" || colors[0].GetColor() != 0x112233 || colors[1].GetName() != "dark_icon" || colors[1].GetColor() != 0xddeeff {
		t.Errorf("colors %v", colors)
	}
	if icons[1].GetColors() != nil {
		t.Errorf("an icon without colors has %v", icons[1].GetColors())
	}
}

// An entry of the side menu alone has no peer types: they share flags.3
// with show_in_attach_menu, so a vector would set the flag and the clients
// would put the bot into the attachment menu (tdesktop
// inline_bots/bot_attach_web_view.cpp:222-227).
func TestAttachMenuBotOfSideMenuOnly(t *testing.T) {
	m := &botstore.AttachMenu{
		BotId: 43, ShortName: "Notes", URL: "https://notes.example/",
		ShowInSideMenu: true, PeerTypes: botstore.AttachPeerAll,
		Icons: []botstore.AttachIcon{{Name: "android_side_menu_static", DocumentId: 601}},
	}
	docs := map[int64]*mtproto.Document{
		601: mtproto.MakeTLDocument(&mtproto.Document{Id: 601, AccessHash: 9, FileReference: []byte{}, MimeType: "image/svg+xml", Size2: 80, DcId: 1, Attributes: []*mtproto.DocumentAttribute{}}).To_Document(),
	}
	bot, missing := attachMenuBotOf(m, docs, false)
	if len(missing) != 0 {
		t.Errorf("missing icons %v", missing)
	}
	got := roundTrip(t, bot)
	if got.GetShowInAttachMenu() || !got.GetShowInSideMenu() || got.GetInactive() || got.GetPeerTypes() != nil {
		t.Errorf("a side menu entry: %v", got)
	}
}

// The methods of the attachment menu are a user's and act on bots only.
func TestAttachMenuTargets(t *testing.T) {
	bot := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 42, AccessHash: 1}).To_InputUser()
	_, err := menuCore(true).MessagesGetAttachMenuBot(&mtproto.TLMessagesGetAttachMenuBot{Bot: bot})
	wantStatus(t, "getAttachMenuBot by a bot", err, "BOT_METHOD_INVALID")
	_, err = menuCore(true).MessagesToggleBotInAttachMenu(&mtproto.TLMessagesToggleBotInAttachMenu{Bot: bot, Enabled: mtproto.BoolTrue})
	wantStatus(t, "toggleBotInAttachMenu by a bot", err, "BOT_METHOD_INVALID")
	for name, in := range map[string]*mtproto.InputUser{
		"self":  mtproto.MakeTLInputUserSelf(nil).To_InputUser(),
		"own":   mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 5}).To_InputUser(),
		"empty": mtproto.MakeTLInputUserEmpty(nil).To_InputUser(),
	} {
		_, err = menuCore(false).MessagesGetAttachMenuBot(&mtproto.TLMessagesGetAttachMenuBot{Bot: in})
		wantStatus(t, "getAttachMenuBot of "+name, err, "BOT_INVALID")
		_, err = menuCore(false).MessagesToggleBotInAttachMenu(&mtproto.TLMessagesToggleBotInAttachMenu{Bot: in, Enabled: mtproto.BoolFalse})
		wantStatus(t, "toggleBotInAttachMenu of "+name, err, "BOT_INVALID")
	}
}
