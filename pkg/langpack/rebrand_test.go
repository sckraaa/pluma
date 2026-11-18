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
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
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
		{"ru", "k", "Добро пожаловать в Telegram, \u2014 Telegram Desktop", "Добро пожаловать в Pluma, \u2014 Pluma"},
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
		// The QR login's links, only in their keys of their pack.
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
		if got := RebrandValue("android", c.lang, c.key, c.in); got != c.want {
			t.Errorf("RebrandValue(android, %s, %s, %q) = %q, want %q", c.lang, c.key, c.in, got, c.want)
		}
	}
}

func TestRebrandPluralized(t *testing.T) {
	one, few := "1 Telegram chat", "%d Telegram chats"
	s := String{Key: "k", Pluralized: true, Value: "%d Telegram chats", One: &one, Few: &few}
	got := Rebrand("android", "ru", s)
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
	kept := Rebrand("tdesktop", "en", String{Key: "lng_about_text1_api", Value: "Telegram API"})
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

// The links of each pack, as the patch of its client writes them.
func TestLinkRules(t *testing.T) {
	for _, c := range []struct {
		pack, key, in, want string
	}{
		{"android", "GroupDescription3", "Public links such as t.me/title", "Public links such as pluma.chat/title"},
		{"ios", "GroupDescription3", "Public links such as t.me/title", "Public links such as t.me/title"},
		{"webk", "GroupDescription3", "Public links such as t.me/title", "Public links such as pluma.chat/title"},
		{"webk", "PublicLinksLimitSubtitle", "Reserve up to %d t.me/username links", "Reserve up to %d pluma.chat/username links"},
		{"android", "PublicLinksLimitSubtitle", "Reserve up to %d t.me/username links", "Reserve up to %d t.me/username links"},
		{"ios", "AuthSessions.AddDevice.ScanInstallInfo",
			"Go to [getdesktop.telegram.org](desktop) or [web.telegram.org](web) to get the QR code",
			"Go to [pluma.chat](desktop) or [web.pluma.chat](web) to get the QR code"},
		{"ios", "Username.LinkHint", "This link opens a chat with you:[\nhttps://t.me/%@]", "This link opens a chat with you:[\nhttps://pluma.chat/%@]"},
		{"ios", "Business.Links.SimpleLinkInfoUsernamePhone",
			"You can also use a simple link for a chat with you \u2014 [t.me/%1$@](username) or [t.me/\u2060+\u2060%2$@](phone).",
			"You can also use a simple link for a chat with you \u2014 [pluma.chat/%1$@](username) or [pluma.chat/\u2060+\u2060%2$@](phone)."},
		{"ios", "SocksProxySetup.QrCode.TMeLink", "t.me link", "pluma.chat link"},
		{"ios", "Username.LinkHint", "at.me and t.mee and x.t.me", "at.me and t.mee and x.t.me"},
		{"webk", "ChatList.Main.EmptyPlaceholder.SubtitleNoContacts",
			"Use Telegram app on your [Android](https://telegram.org/android) or [iOS](https://telegram.org/dl/ios) device to sync your contacts",
			"Use Pluma app on your Android or iOS device to sync your contacts"},
		{"webk", "ChatAutomation.HeroDescription",
			"Add a bot to your account to help you automatically process and respond to the messages you receive. [Learn More >]()",
			"Add a bot to your account to help you automatically process and respond to the messages you receive."},
		{"tdesktop", "lng_settings_section_privacy", "t.me/x telegram.org", "t.me/x telegram.org"},
	} {
		if got := RebrandValue(c.pack, "en", c.key, c.in); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.pack, c.key, got, c.want)
		}
	}
}

// In every language of Telegram's packs (testdata/links.json: the strings of
// linkRules on 2026-11-18), no string of the table keeps a link to Telegram's
// sites, a t.me link or an empty link after the replacement.
func TestLinkRulesInEveryLanguage(t *testing.T) {
	data, err := os.ReadFile("testdata/links.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]map[string]map[string][]string
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	telegramLink := regexp.MustCompile(`(?:^|[^\w.])t\.me(?:$|[^\w])|telegram\.org`)
	for pack, keys := range linkRules {
		for key := range keys {
			texts := snap[pack][key]
			if len(texts) < 20 {
				t.Errorf("%s/%s: %d languages in testdata", pack, key, len(texts))
			}
			for lang, values := range texts {
				for _, v := range values {
					got := RebrandValue(pack, lang, key, v)
					if telegramLink.MatchString(got) || key == "ChatAutomation.HeroDescription" && strings.Contains(got, "]()") ||
						key == "ChatList.Main.EmptyPlaceholder.SubtitleNoContacts" && strings.ContainsAny(got, "[]") {
						t.Errorf("%s/%s in %s: %q", pack, key, lang, got)
					}
					if strings.Contains(v, "t.me") && !strings.Contains(got, "pluma.chat") {
						t.Errorf("%s/%s in %s: no pluma.chat in %q", pack, key, lang, got)
					}
				}
			}
		}
	}
}
