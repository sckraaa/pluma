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

// Package servicetext holds the messages of the service notifications
// account (777000) as Telegram writes them (task 019,
// docs/tasks/019-service-messages.md): the same words, line breaks and bold
// text, with "Telegram" replaced by the name of the app. A language without
// a text known from Telegram gets the English one, as Telegram did for a
// client in Czech.
package servicetext

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// Lang is a language of the texts.
type Lang string

const (
	English Lang = "en"
	Russian Lang = "ru"
)

// LangOf is the language of the texts for the lang_code of initConnection
// ("ru", "ru-RU", "en"...).
func LangOf(langCode string) Lang {
	code := strings.ToLower(langCode)
	if code == "ru" || strings.HasPrefix(code, "ru-") || strings.HasPrefix(code, "ru_") {
		return Russian
	}
	return English
}

// Render builds the text of tpl and its bold entities: "**" starts and
// ends bold text, and {key} is params[key]. The values are put in as they
// are, never read as markup. Offsets and lengths are in UTF-16 code units,
// as the API counts them.
func Render(tpl string, params map[string]string) (string, []*mtproto.MessageEntity) {
	var (
		text     strings.Builder
		entities []*mtproto.MessageEntity
		at       int // UTF-16 length of text
		boldAt   = -1
	)
	write := func(s string) {
		text.WriteString(s)
		at += len(utf16.Encode([]rune(s)))
	}
	for len(tpl) > 0 {
		switch {
		case strings.HasPrefix(tpl, "**"):
			if boldAt < 0 {
				boldAt = at
			} else {
				if at > boldAt {
					entities = append(entities, mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{
						Offset: int32(boldAt),
						Length: int32(at - boldAt),
					}).To_MessageEntity())
				}
				boldAt = -1
			}
			tpl = tpl[2:]
		case tpl[0] == '{':
			end := strings.IndexByte(tpl, '}')
			if end < 0 {
				panic(fmt.Sprintf("servicetext: unclosed {} in %q", tpl))
			}
			key := tpl[1:end]
			v, ok := params[key]
			if !ok {
				panic(fmt.Sprintf("servicetext: no value for {%s}", key))
			}
			write(v)
			tpl = tpl[end+1:]
		default:
			next := len(tpl)
			if i := strings.Index(tpl, "**"); i >= 0 && i < next {
				next = i
			}
			if i := strings.IndexByte(tpl, '{'); i >= 0 && i < next {
				next = i
			}
			write(tpl[:next])
			tpl = tpl[next:]
		}
	}
	if boldAt >= 0 {
		panic("servicetext: unclosed ** in a template")
	}
	return text.String(), entities
}

// The login code ("app" is the name of the app), as Telegram sent it in
// April (Russian) and October (English) 2026.
var loginCode = map[Lang]string{
	English: "**Login code:** {code}. Do **not** give this code to anyone, even if they say they are from {app}!\n\n" +
		"❗️This code can be used to log in to your {app} account. We never ask it for anything else.\n\n" +
		"If you didn't request this code by trying to log in on another device, simply ignore this message.",
	// The space after "ещё." is Telegram's.
	Russian: "**Код для входа в {app}:** {code}. Не давайте код **никому**, даже если его требуют от имени {app}!\n\n" +
		"❗️Этот код используется для входа в Ваш аккаунт в {app}. Он не может быть использован для чего-либо ещё. \n\n" +
		"Если Вы не запрашивали код для входа, проигнорируйте это сообщение.",
}

// LoginCode is the message with the login code sent to the app.
func LoginCode(lang Lang, code string) (string, []*mtproto.MessageEntity) {
	return Render(textOf(loginCode, lang), map[string]string{"code": code, "app": env2.MyAppName})
}

// The New login message, as Telegram sent it in 2025 and 2026 (English) and
// on 2026-10-29 (Russian). The last English paragraph comes only to
// accounts without a cloud password; Telegram's Russian one is not known,
// so such an account gets the English message.
var (
	newLogin = map[Lang]string{
		English: "**New login.** Dear {name}, we detected a login into your account from a new device on {date} at {time} UTC.\n\n" +
			"Device: {device}\nLocation: {location}\n\n" +
			"If this wasn't you, you can terminate that session in **Settings > Devices** (or **Privacy & Security > Active Sessions**).",
		Russian: "**Вход с нового устройства.** {name}, мы обнаружили вход в Ваш аккаунт с нового устройства {date} в {time} UTC.\n\n" +
			"Устройство: {device}\nМесто входа: {location}\n\n" +
			"Если это были не Вы, как можно скорее перейдите в **Настройки > Устройства** (или **Конфиденциальность > Активные сеансы**) и завершите новый сеанс.",
	}
	newLoginNoPassword = map[Lang]string{
		English: "\n\nIf you think that somebody logged in to your account against your will, you can enable **Two-Step Verification** in Privacy and Security settings.",
	}
)

// NewLogin is a login of the account from a new device.
type NewLogin struct {
	Name        string    // of the account
	Date        time.Time // of the login
	Device      string    // "app, version, model, system"
	Location    string    // "City, Country" or "Country"
	HasPassword bool      // the account has a cloud password
}

// Text is the New login message in lang.
func (n NewLogin) Text(lang Lang) (string, []*mtproto.MessageEntity) {
	tpl := textOf(newLogin, lang)
	if !n.HasPassword {
		tail, ok := newLoginNoPassword[lang]
		if !ok {
			tpl, tail = newLogin[English], newLoginNoPassword[English]
		}
		tpl += tail
	}
	date := n.Date.UTC()
	return Render(tpl, map[string]string{
		"name":     n.Name,
		"date":     date.Format("02/01/2006"),
		"time":     date.Format("15:04:05"),
		"device":   n.Device,
		"location": n.Location,
	})
}

func textOf(texts map[Lang]string, lang Lang) string {
	if t, ok := texts[lang]; ok {
		return t
	}
	return texts[English]
}
