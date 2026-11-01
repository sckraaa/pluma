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

package langpack

import (
	"fmt"
	"testing"
)

func TestRebrandValue(t *testing.T) {
	for _, c := range []struct {
		lang, key, in, want string
	}{
		// The word rule of the Android patch.
		{"en", "k", "Welcome to Telegram!", "Welcome to Pluma!"},
		{"en", "k", "TELEGRAM PREMIUM", "PLUMA PREMIUM"},
		{"en", "k", "Telegram's servers", "Pluma's servers"},
		{"en", "k", "**Telegram Premium**", "**Pluma Premium**"},
		{"en", "k", "Telegram.", "Pluma."},
		{"en", "k", "line\nTelegram", "line\nPluma"},
		{"en", "k", "t.me/TelegramTips", "t.me/TelegramTips"},
		{"en", "k", "@Telegram", "@Telegram"},
		{"en", "k", "Visit Telegram.org", "Visit Telegram.org"},
		{"de", "k", "Telegramm", "Telegramm"},
		{"en", "k", "https://telegram.org/faq", "https://telegram.org/faq"},
		{"en", "k", "MyTelegram", "MyTelegram"},
		{"en", "k", "Telegram2", "Telegram2"},
		{"en", "k", "telegram", "telegram"},
		{"ru", "k", "Добро пожаловать в Telegram, — Telegram Desktop", "Добро пожаловать в Pluma, — Pluma"},
		{"en", "k", "Telegram Desktop2", "Pluma Desktop2"},
		{"en", "k", "Telegram Desktop.", "Pluma."},
		{"en", "lng_about_text_1", `based on [a href="https://core.telegram.org/api"]Telegram API[/a]`,
			`based on [a href="https://core.telegram.org/api"]Pluma API[/a]`},
		// Inflected forms take the same form of Pluma (inflect_test.go); an
		// ending the language does not list stays (and is reported).
		{"cs", "k", "Vítejte v Telegramu", "Vítejte v Plumě"},
		{"cs", "k", "Telegramech", "Telegramech"},
		{"en", "k", "Telegramu", "Telegramu"},
		{"sk", "k", "v Telegramách", "v Telegramách"},
		{"ru", "k", "Telegramом", "Telegramом"},
		{"zh-hans", "k", "Telegram上的任何人", "Pluma上的任何人"},
		{"he", "k", "לTelegram", "לPluma"},
		// The keep-list.
		{"en", "lng_about_text1_api", "Telegram API", "Telegram API"},
		// Local names, longest first; only in their language.
		{"ar", "k", "مرحبا بك في تيليجرام لتيليجرام تليجرام", "مرحبا بك في Pluma لـPluma Pluma"},
		{"fa", "k", "به تلگرام خوش آمدید", "به Pluma خوش آمدید"},
		{"ko", "k", "텔레그램이 텔레그램을 텔레그램", "Pluma가 Pluma를 Pluma"},
		{"ko", "k", "텔레그램이라는", "Pluma라는"},
		{"en", "k", "텔레그램", "텔레그램"},
		{"nl", "k", "Telegramberichten en Telegram", "Pluma-berichten en Pluma"},
		{"nl-raw", "k", "Telegramberichten", "Pluma-berichten"},
		// The QR login's links, only in their keys.
		{"en", "AuthAnotherClientDownloadClientUrl", "https://getdesktop.telegram.org/", "https://pluma.chat/"},
		{"ar", "AuthAnotherClientDownloadClientUrl", "https://desktop.telegram.org", "https://pluma.chat/"},
		{"de", "AuthAnotherClientDownloadClientUrl", "https://desktop.telegram.org/", "https://pluma.chat/"},
		{"de", "AuthAnotherWebClientUrl", "https://web.telegram.org/", "https://web.pluma.chat/"},
		{"en", "AuthAnotherClientInfo1", "Download Telegram on your computer from *getdesktop.telegram.org*",
			"Download Pluma on your computer from *pluma.chat*"},
		{"en", "AuthAnotherClientInfo5", "Go to *web.telegram.org*", "Go to *web.pluma.chat*"},
		{"en", "OtherKey", "Go to *web.telegram.org*", "Go to *web.telegram.org*"},
		// The sentence that would turn false.
		{"en", "k", "Some videos can't be streamed – e.g., all videos that were sent from Android using Telegram 4.7 and earlier versions.",
			"Some videos can't be streamed – e.g., videos that were sent from very old versions of the app."},
	} {
		if got := RebrandValue(c.lang, c.key, c.in); got != c.want {
			t.Errorf("RebrandValue(%s, %s, %q) = %q, want %q", c.lang, c.key, c.in, got, c.want)
		}
	}
}

func TestRebrandPluralized(t *testing.T) {
	one, few := "1 Telegram chat", "%d Telegram chats"
	s := String{Key: "k", Pluralized: true, Value: "%d Telegram chats", One: &one, Few: &few}
	got := Rebrand("ru", s)
	if got.Key != "k" || got.Value != "%d Pluma chats" || *got.One != "1 Pluma chat" || *got.Few != "%d Pluma chats" || got.Zero != nil {
		t.Fatalf("Rebrand = %+v", got)
	}
	if one != "1 Telegram chat" {
		t.Fatal("Rebrand changed its argument")
	}
}

// The import's report lists what the rules missed, not the strings kept as
// Telegram wrote them on purpose.
func TestLeftoverWords(t *testing.T) {
	few := "%d Telegramy"
	for _, c := range []struct {
		s    String
		want string
	}{
		{String{Key: "lng_about_text1_api", Value: "Telegram API"}, "[]"},
		{String{Key: "lng_about_text2", Value: "Telegram API"}, "[Telegram]"},
		{String{Key: "Welcome", Value: "Vítejte v Pluma"}, "[]"},
		{String{Key: "Chats", Pluralized: true, Value: "%d Telegramu", Few: &few, Many: &few}, "[Telegramu Telegramy]"},
	} {
		if got := fmt.Sprint(leftoverWords(&c.s)); got != c.want {
			t.Errorf("leftoverWords(%s) = %s, want %s", c.s.Key, got, c.want)
		}
	}
	// What an import reports for a kept key: its value unchanged, no words.
	kept := Rebrand("en", String{Key: "lng_about_text1_api", Value: "Telegram API"})
	if kept.Value != "Telegram API" || leftoverWords(&kept) != nil {
		t.Errorf("kept key: %q, %v", kept.Value, leftoverWords(&kept))
	}
}

func TestLeftovers(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"Welcome to Pluma", "[]"},
		{"Vítejte v Telegramu, Telegramem i telegram", "[Telegramu Telegramem telegram]"},
		{"Добро пожаловать в Телеграм и телеграме", "[Телеграм телеграме]"},
		{"Visit https://telegram.org/faq or t.me/TelegramTips or @Telegram or Telegram.org", "[]"},
		{`[a href="https://core.telegram.org/api"]Pluma API[/a]`, "[]"},
		{"Go to *web.telegram.org*", "[]"},
		{"텔레그램 또는 تيليجرام", "[텔레그램 تيليجرام]"},
		{"Telegram API", "[Telegram]"},
	} {
		if got := fmt.Sprint(Leftovers(c.in)); got != c.want {
			t.Errorf("Leftovers(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
