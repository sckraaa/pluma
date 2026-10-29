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

package servicetext

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
)

// bold returns the bold parts of text, cut by the UTF-16 offsets of
// entities as a client cuts them.
func bold(t *testing.T, text string, entities []*mtproto.MessageEntity) []string {
	t.Helper()
	units := utf16.Encode([]rune(text))
	var parts []string
	for _, e := range entities {
		if e.GetPredicateName() != mtproto.Predicate_messageEntityBold {
			t.Fatalf("entity %s, want bold", e.GetPredicateName())
		}
		end := int(e.GetOffset() + e.GetLength())
		if e.GetOffset() < 0 || end > len(units) {
			t.Fatalf("entity %d+%d outside %d units", e.GetOffset(), e.GetLength(), len(units))
		}
		parts = append(parts, string(utf16.Decode(units[e.GetOffset():end])))
	}
	return parts
}

func equal(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func TestLangOf(t *testing.T) {
	for code, want := range map[string]Lang{
		"ru": Russian, "ru-RU": Russian, "RU": Russian, "ru_RU": Russian,
		"en": English, "cs": English, "": English, "rue": English,
	} {
		if got := LangOf(code); got != want {
			t.Errorf("LangOf(%q) = %s, want %s", code, got, want)
		}
	}
}

func TestRenderKeepsValues(t *testing.T) {
	text, entities := Render("**Hi** {name}!", map[string]string{"name": "**Анна** {x}"})
	if text != "Hi **Анна** {x}!" {
		t.Fatalf("text %q", text)
	}
	if got := bold(t, text, entities); !equal(got, []string{"Hi"}) {
		t.Fatalf("bold %q", got)
	}
}

func TestLoginCode(t *testing.T) {
	text, entities := LoginCode(English, "12345")
	want := "Login code: 12345. Do not give this code to anyone, even if they say they are from Pluma!\n\n" +
		"❗️This code can be used to log in to your Pluma account. We never ask it for anything else.\n\n" +
		"If you didn't request this code by trying to log in on another device, simply ignore this message."
	if text != want {
		t.Fatalf("English text %q", text)
	}
	if got := bold(t, text, entities); !equal(got, []string{"Login code:", "not"}) {
		t.Fatalf("English bold %q", got)
	}

	text, entities = LoginCode(Russian, "12345")
	want = "Код для входа в Pluma: 12345. Не давайте код никому, даже если его требуют от имени Pluma!\n\n" +
		"❗️Этот код используется для входа в Ваш аккаунт в Pluma. Он не может быть использован для чего-либо ещё. \n\n" +
		"Если Вы не запрашивали код для входа, проигнорируйте это сообщение."
	if text != want {
		t.Fatalf("Russian text %q", text)
	}
	if got := bold(t, text, entities); !equal(got, []string{"Код для входа в Pluma:", "никому"}) {
		t.Fatalf("Russian bold %q", got)
	}
}

func TestNewLogin(t *testing.T) {
	n := NewLogin{
		Name:        ".",
		Date:        time.Date(2026, 10, 29, 16, 46, 6, 0, time.FixedZone("CET", 3600)),
		Device:      "Pluma Web, 12.0.48 A, Safari 26, macOS",
		Location:    "Prague, Czechia",
		HasPassword: true,
	}
	text, entities := n.Text(Russian)
	want := "Вход с нового устройства. ., мы обнаружили вход в Ваш аккаунт с нового устройства 29/10/2026 в 15:46:06 UTC.\n\n" +
		"Устройство: Pluma Web, 12.0.48 A, Safari 26, macOS\nМесто входа: Prague, Czechia\n\n" +
		"Если это были не Вы, как можно скорее перейдите в Настройки > Устройства (или Конфиденциальность > Активные сеансы) и завершите новый сеанс."
	if text != want {
		t.Fatalf("Russian text %q", text)
	}
	if got := bold(t, text, entities); !equal(got, []string{"Вход с нового устройства.", "Настройки > Устройства", "Конфиденциальность > Активные сеансы"}) {
		t.Fatalf("Russian bold %q", got)
	}

	text, entities = n.Text(English)
	want = "New login. Dear ., we detected a login into your account from a new device on 29/10/2026 at 15:46:06 UTC.\n\n" +
		"Device: Pluma Web, 12.0.48 A, Safari 26, macOS\nLocation: Prague, Czechia\n\n" +
		"If this wasn't you, you can terminate that session in Settings > Devices (or Privacy & Security > Active Sessions)."
	if text != want {
		t.Fatalf("English text %q", text)
	}
	if got := bold(t, text, entities); !equal(got, []string{"New login.", "Settings > Devices", "Privacy & Security > Active Sessions"}) {
		t.Fatalf("English bold %q", got)
	}
}

func TestNewLoginWithoutPassword(t *testing.T) {
	n := NewLogin{Name: "Anna", Date: time.Unix(0, 0), Device: "d", Location: "l"}
	tail := "\n\nIf you think that somebody logged in to your account against your will, you can enable Two-Step Verification in Privacy and Security settings."
	for _, lang := range []Lang{English, Russian} {
		// No Russian text of Telegram has the paragraph: the English
		// message, whole.
		text, entities := n.Text(lang)
		if !strings.HasPrefix(text, "New login. Dear Anna,") || !strings.HasSuffix(text, tail) {
			t.Fatalf("%s: text %q", lang, text)
		}
		got := bold(t, text, entities)
		if len(got) != 4 || got[3] != "Two-Step Verification" {
			t.Fatalf("%s: bold %q", lang, got)
		}
	}
}
