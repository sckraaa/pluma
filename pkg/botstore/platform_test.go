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

package botstore

import (
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestValidShortName(t *testing.T) {
	for name, want := range map[string]bool{
		"game": true, "My_App_2": true, strings.Repeat("a", 64): true,
		"": false, strings.Repeat("a", 65): false, "with-dash": false, "dot.ted": false, "пробел": false, "a b": false,
	} {
		if got := ValidShortName(name); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
}

func TestNormalizeLoginDomain(t *testing.T) {
	ok := map[string]string{
		"Example.COM":          "example.com",
		"login.example.co.uk":  "login.example.co.uk",
		"xn--80ak6aa92e.com":   "xn--80ak6aa92e.com",
		"a-b.example.org":      "a-b.example.org",
		"1password.example.io": "1password.example.io",
	}
	for in, want := range ok {
		if got, valid := NormalizeLoginDomain(in); !valid || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, valid, want)
		}
	}
	for _, in := range []string{
		"", "localhost", "example.com.", ".example.com", "example..com", "-a.example.com", "a-.example.com",
		"example.com:443", "https://example.com", "example.com/path", "10.0.0.1", "[::1]", "ex_ample.com",
		strings.Repeat("a", 64) + ".com", strings.Repeat("abcdefghi.", 26) + "com",
	} {
		if got, valid := NormalizeLoginDomain(in); valid {
			t.Errorf("%q passed as %q", in, got)
		}
	}
}

func TestLoginClientAddresses(t *testing.T) {
	if err := checkLoginClient([]string{"https://site.example/cb?x=1"}, []string{"https://site.example", "https://app.site.example:8443"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"http://site.example/cb", "https://site.example/cb#frag", "/cb", "https://user@site.example/cb", "site.example/cb"} {
		if err := checkLoginClient([]string{r}, nil); !errors.Is(err, ErrLoginClientInvalid) {
			t.Errorf("redirect %q: %v", r, err)
		}
	}
	for _, o := range []string{"https://site.example/", "https://Site.example", "http://site.example", "https://site.example/path", "https://site.example?q"} {
		if err := checkLoginClient([]string{"https://site.example/cb"}, []string{o}); !errors.Is(err, ErrLoginClientInvalid) {
			t.Errorf("origin %q: %v", o, err)
		}
	}
	if err := checkLoginClient(nil, nil); !errors.Is(err, ErrLoginClientInvalid) {
		t.Errorf("no redirect: %v", err)
	}
}

func TestAttachMenuValidate(t *testing.T) {
	good := func() AttachMenu {
		return AttachMenu{
			BotId: 1, ShortName: "menu", URL: "https://app.example", ShowInAttachMenu: true, PeerTypes: AttachPeerPM | AttachPeerChat,
			Icons: []AttachIcon{{Name: "default_static", DocumentId: 5}, {Name: "android_animated", DocumentId: 6, Colors: []AttachIconColor{{Name: "light_icon", Color: 0xff}}}},
		}
	}
	m := good()
	if err := m.validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*AttachMenu){
		"http url":         func(m *AttachMenu) { m.URL = "http://app.example" },
		"shown nowhere":    func(m *AttachMenu) { m.ShowInAttachMenu = false },
		"no peer types":    func(m *AttachMenu) { m.PeerTypes = 0 },
		"unknown peer bit": func(m *AttachMenu) { m.PeerTypes |= 1 << 7 },
		"no default icon":  func(m *AttachMenu) { m.Icons = m.Icons[1:] },
		"unknown icon":     func(m *AttachMenu) { m.Icons[1].Name = "windows_static" },
		"repeated icon":    func(m *AttachMenu) { m.Icons[1].Name = "default_static" },
		"icon w/o doc":     func(m *AttachMenu) { m.Icons[0].DocumentId = 0 },
		"unknown color":    func(m *AttachMenu) { m.Icons[1].Colors[0].Name = "mid_icon" },
		"bad short name":   func(m *AttachMenu) { m.ShortName = "a-b" },
	}
	for name, change := range cases {
		m := good()
		change(&m)
		if err := m.validate(); err == nil {
			t.Errorf("%s passed", name)
		}
	}
	// The side menu alone needs no peer types and no default_static.
	side := AttachMenu{ShortName: "side", URL: "https://app.example", ShowInSideMenu: true}
	if err := side.validate(); err != nil {
		t.Errorf("side menu: %v", err)
	}
}

func TestAttachMenusHash(t *testing.T) {
	if AttachMenusHash(nil) != 0 {
		t.Error("empty list hashes to 0")
	}
	a := []AttachMenu{{BotId: 10, PlatformVersion: 1}, {BotId: 11, PlatformVersion: 4}}
	b := []AttachMenu{{BotId: 10, PlatformVersion: 2}, {BotId: 11, PlatformVersion: 4}}
	c := []AttachMenu{{BotId: 11, PlatformVersion: 4}, {BotId: 10, PlatformVersion: 1}}
	if AttachMenusHash(a) == AttachMenusHash(b) {
		t.Error("a new version of a bot keeps the hash")
	}
	if AttachMenusHash(a) == AttachMenusHash(c) {
		t.Error("another order keeps the hash")
	}
	if AttachMenusHash(a) != AttachMenusHash(append([]AttachMenu{}, a...)) {
		t.Error("the hash is not stable")
	}
}

func TestScoreChatOf(t *testing.T) {
	one, err := ScoreChatOf(7, mtproto.PEER_USER, 3)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := ScoreChatOf(3, mtproto.PEER_USER, 7)
	if one != two || one != (ScoreChat{Type: mtproto.PEER_USER, Id: 3, Id2: 7}) {
		t.Errorf("private chat: %+v and %+v", one, two)
	}
	if self, _ := ScoreChatOf(5, mtproto.PEER_SELF, 5); self != (ScoreChat{Type: mtproto.PEER_USER, Id: 5, Id2: 5}) {
		t.Errorf("self: %+v", self)
	}
	if ch, _ := ScoreChatOf(5, mtproto.PEER_CHANNEL, 99); ch != (ScoreChat{Type: mtproto.PEER_CHANNEL, Id: 99}) {
		t.Errorf("channel: %+v", ch)
	}
	if _, err := ScoreChatOf(5, mtproto.PEER_EMPTY, 0); err == nil {
		t.Error("empty peer passed")
	}
}

func TestWidgetHelpers(t *testing.T) {
	if WidgetDataHash("a") == WidgetDataHash("b") || WidgetDataHash("a") != WidgetDataHash("a") || WidgetDataHash("") == 0 {
		t.Error("data hash")
	}
	for in, want := range map[int32]int32{0: DefaultWidgetPeriod, -1: DefaultWidgetPeriod, 1: 5, 4: 5, 5: 5, 30: 30, 3600: 3600, 99999: 3600} {
		if got := ClampWidgetPeriod(in); got != want {
			t.Errorf("period %d: %d, want %d", in, got, want)
		}
	}
}

func TestCut(t *testing.T) {
	if got := cut("привет", 3); got != "при" {
		t.Errorf("cut: %q", got)
	}
	if got := cut("ok", 3); got != "ok" {
		t.Errorf("short: %q", got)
	}
}
