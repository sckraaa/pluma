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

// withOwn replaces Pluma's own strings for one test.
func withOwn(t *testing.T, data string) {
	t.Helper()
	own, err := loadOwn([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	saved := ownStrings
	ownStrings = own
	t.Cleanup(func() { ownStrings = saved })
}

// withOverride replaces the strings that take the place of Telegram's for one
// test.
func withOverride(t *testing.T, data string) {
	t.Helper()
	override, err := loadOwn([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	saved := overrideStrings
	overrideStrings = override
	t.Cleanup(func() { overrideStrings = saved })
}

func TestOwnJSONIsValid(t *testing.T) {
	if _, err := loadOwn(ownJSON); err != nil {
		t.Fatal(err)
	}
}

// override.json is valid, shares no key with own.json and has every key in
// every language of own.json: a language left out would show the English text
// in place of Telegram's translation.
func TestOverrideJSONIsValid(t *testing.T) {
	own, err := loadOwn(ownJSON)
	if err != nil {
		t.Fatal(err)
	}
	override, err := loadOwn(overrideJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := disjoint(own, override); err != nil {
		t.Fatal(err)
	}
	langs := map[string]bool{}
	for _, keys := range own {
		for _, texts := range keys {
			for lang := range texts {
				langs[lang] = true
			}
		}
	}
	for pack, keys := range override {
		for key, texts := range keys {
			for lang := range langs {
				if texts[lang] == "" {
					t.Errorf("%s/%s: no text in %s", pack, key, lang)
				}
			}
		}
	}
}

// The status line of a support account says "service account" in the four
// clients' packs.
func TestOverrideSupportStatus(t *testing.T) {
	override, err := loadOwn(overrideJSON)
	if err != nil {
		t.Fatal(err)
	}
	for pack, key := range map[string]string{
		"android":  "SupportStatus",
		"ios":      "Bot.GenericSupportStatus",
		"tdesktop": "lng_status_support",
		"webk":     "SupportStatus",
	} {
		for lang, want := range map[string]string{"en": "service account", "ru": "сервисный аккаунт", "pt-br": "conta de serviço"} {
			strs, missing := textsFor(override, pack, lang)
			found := false
			for _, s := range strs {
				if s.Key == key {
					found = s.Value == want
				}
			}
			if !found || len(missing) != 0 {
				t.Errorf("%s/%s in %s: %v, missing %v", pack, key, lang, strs, missing)
			}
		}
	}
}

// The four clients show the same words in a language: one table of
// translations, whichever pack a client reads (Web K reads "webk" and
// "android").
func TestOverrideSupportStatusIsOneTable(t *testing.T) {
	override, err := loadOwn(overrideJSON)
	if err != nil {
		t.Fatal(err)
	}
	table := override["android"]["SupportStatus"]
	if len(table) == 0 {
		t.Fatal("no android/SupportStatus")
	}
	for pack, key := range map[string]string{
		"ios":      "Bot.GenericSupportStatus",
		"tdesktop": "lng_status_support",
		"webk":     "SupportStatus",
	} {
		texts := override[pack][key]
		if len(texts) != len(table) {
			t.Errorf("%s/%s: %d languages, android has %d", pack, key, len(texts), len(table))
		}
		for lang, want := range table {
			if texts[lang] != want {
				t.Errorf("%s/%s in %s: %q, android has %q", pack, key, lang, texts[lang], want)
			}
		}
	}
}

func TestOverrideSharesNoKeyWithOwn(t *testing.T) {
	own, err := loadOwn([]byte(`{"ios": {"Pluma.A": {"en": "x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for data, ok := range map[string]bool{
		`{"ios": {"Pluma.A": {"en": "y"}}}`:    false,
		`{"ios": {"Bot.Status": {"en": "y"}}}`: true,
		`{"webk": {"Pluma.A": {"en": "y"}}}`:   true,
	} {
		override, err := loadOwn([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if err := disjoint(own, override); (err == nil) != ok {
			t.Errorf("%s: %v", data, err)
		}
	}
}

func TestOwnRejects(t *testing.T) {
	for _, data := range []string{
		`not json`,
		`{"IOS": {"k": {"en": "x"}}}`,
		`{"ios": {"k": {"ru": "x"}}}`,
		`{"ios": {"": {"en": "x"}}}`,
		`{"ios": {"k": {"en": "x", "RU": "y"}}}`,
		`{"ios": {"k": {"en": "x", "ru": ""}}}`,
	} {
		if _, err := loadOwn([]byte(data)); err == nil {
			t.Errorf("%s: accepted", data)
		}
	}
}

// A language gets its own text, the base language's, or the English one.
func TestOwnForFallsBack(t *testing.T) {
	withOwn(t, `{"ios": {
		"Pluma.A": {"en": "Check Telegram", "ru": "Проверьте Telegram", "pt-br": "Verifique o Telegram"},
		"Pluma.B": {"en": "Send again", "pt": "Reenviar"}
	}}`)
	for _, tt := range []struct{ lang, want, missing string }{
		{"en", "[Pluma.A=Check Telegram Pluma.B=Send again]", "[]"},
		{"ru", "[Pluma.A=Проверьте Telegram Pluma.B=Send again]", "[Pluma.B]"},
		{"pt-br", "[Pluma.A=Verifique o Telegram Pluma.B=Reenviar]", "[]"},
		{"fa", "[Pluma.A=Check Telegram Pluma.B=Send again]", "[Pluma.A Pluma.B]"},
	} {
		strs, missing := ownFor("ios", tt.lang)
		var got []string
		for _, s := range strs {
			got = append(got, s.Key+"="+s.Value)
		}
		if fmt.Sprint(got) != tt.want || fmt.Sprint(missing) != tt.missing {
			t.Errorf("%s: %v, missing %v", tt.lang, got, missing)
		}
	}
	if strs, _ := ownFor("android", "ru"); len(strs) != 0 {
		t.Fatalf("android: %v", strs)
	}
}
