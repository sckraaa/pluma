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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
)

func TestClientIP(t *testing.T) {
	for addr, want := range map[string]string{
		"193.17.47.1":       "193.17.47.1",
		"193.17.47.1:51234": "193.17.47.1",
		"[2001:db8::1]:443": "2001:db8::1",
		"2001:db8::1":       "2001:db8::1",
		"":                  "",
		"gateway":           "",
	} {
		if got := clientIP(addr); got != want {
			t.Errorf("%q: %q, want %q", addr, got, want)
		}
	}
}

// A site logged in on from an app shows the app with its version and the
// device with its system.
func TestSessionNames(t *testing.T) {
	browser, platform := sessionNames(&mtproto.Authorization{AppName: "Pluma Android", AppVersion: "12.1.1", DeviceModel: "Pixel 8", Platform: "Android", SystemVersion: "15 (35)"})
	if browser != "Pluma Android 12.1.1" || platform != "Pixel 8, Android" {
		t.Errorf("Android: %q, %q", browser, platform)
	}
	browser, platform = sessionNames(&mtproto.Authorization{AppName: "Pluma Web", AppVersion: "2.2 K", DeviceModel: "Safari 26", SystemVersion: "macOS"})
	if browser != "Pluma Web 2.2 K" || platform != "Safari 26, macOS" {
		t.Errorf("Web K: %q, %q", browser, platform)
	}
	if browser, platform = sessionNames(&mtproto.Authorization{}); browser != "" || platform != "" {
		t.Errorf("nothing known: %q, %q", browser, platform)
	}
}

// The permission message of a user and a bot has one random_id, whatever
// let the bot write and however often.
func TestBotAllowedRandomId(t *testing.T) {
	a := botAllowedRandomId(10, 20)
	if a <= 0 || a != botAllowedRandomId(10, 20) {
		t.Fatalf("random_id %d is not stable and positive", a)
	}
	if a == botAllowedRandomId(20, 10) || a == botAllowedRandomId(10, 21) {
		t.Error("another pair has the same random_id")
	}
}

func TestLoginButtonOf(t *testing.T) {
	login := mtproto.MakeTLKeyboardButtonUrlAuth(&mtproto.KeyboardButton{Text: "Log in", Url: "https://site.example", ButtonId: 2, UserId_INT64: 20}).To_KeyboardButton()
	link := mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "Site", Url: "https://site.example"}).To_KeyboardButton()
	m := mtproto.MakeTLMessage(&mtproto.Message{ReplyMarkup: mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{link, login}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()}).To_Message()
	if b := loginButtonOf(m, 2); b != login {
		t.Errorf("button 2: %v", b)
	}
	if b := loginButtonOf(m, 1); b != nil {
		t.Errorf("button 1, a plain link: %v", b)
	}
	if b := loginButtonOf(mtproto.MakeTLMessage(&mtproto.Message{}).To_Message(), 2); b != nil {
		t.Errorf("no markup: %v", b)
	}
}

// What a request names is checked before anything is looked up: a login
// names a button or a link, in_app_origin belongs to the logins of sites
// through oauth.pluma.chat, and a link carries the data only over https.
func TestUrlAuthTargetArguments(t *testing.T) {
	c := testCore(&dao.Dao{}, 10)
	origin, plain, ftp := "https://app.example", "http://site.example/login", "ftp://site.example"
	msgId := int32(5)
	for name, call := range map[string]func() error{
		"nothing": func() error {
			_, err := c.urlAuthTarget(nil, nil, nil, nil, nil)
			return err
		},
		"a message without a button": func() error {
			_, err := c.urlAuthTarget(mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 20}).To_InputPeer(), &msgId, nil, nil, nil)
			return err
		},
		"in_app_origin": func() error {
			_, err := c.urlAuthTarget(nil, nil, nil, &plain, &origin)
			return err
		},
		"http link": func() error {
			_, err := c.urlAuthTarget(nil, nil, nil, &plain, nil)
			return err
		},
		"ftp link": func() error {
			_, err := c.urlAuthTarget(nil, nil, nil, &ftp, nil)
			return err
		},
	} {
		if err := call(); !errors.Is(err, mtproto.ErrUrlInvalid) {
			t.Errorf("%s: %v, want URL_INVALID", name, err)
		}
	}
}
