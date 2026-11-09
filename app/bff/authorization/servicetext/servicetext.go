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
	"sort"
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

// Render builds the text of tpl and its entities: "**" starts and ends
// bold text, "||" a spoiler, and {key} is params[key]. The values are put
// in as they are, never read as markup. Offsets and lengths are in UTF-16
// code units, as the API counts them; the entities are in the order of
// their offsets.
func Render(tpl string, params map[string]string) (string, []*mtproto.MessageEntity) {
	var (
		text     strings.Builder
		entities []*mtproto.MessageEntity
		at       int // UTF-16 length of text
		open     = map[string]int{}
	)
	write := func(s string) {
		text.WriteString(s)
		at += len(utf16.Encode([]rune(s)))
	}
	toggle := func(marker string) {
		start, ok := open[marker]
		if !ok {
			open[marker] = at
			return
		}
		delete(open, marker)
		if at == start {
			return
		}
		e := &mtproto.MessageEntity{Offset: int32(start), Length: int32(at - start)}
		if marker == "**" {
			entities = append(entities, mtproto.MakeTLMessageEntityBold(e).To_MessageEntity())
		} else {
			entities = append(entities, mtproto.MakeTLMessageEntitySpoiler(e).To_MessageEntity())
		}
	}
	for len(tpl) > 0 {
		switch {
		case strings.HasPrefix(tpl, "**"), strings.HasPrefix(tpl, "||"):
			toggle(tpl[:2])
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
			for _, m := range []string{"**", "||", "{"} {
				if i := strings.Index(tpl, m); i >= 0 && i < next {
					next = i
				}
			}
			write(tpl[:next])
			tpl = tpl[next:]
		}
	}
	if len(open) > 0 {
		panic(fmt.Sprintf("servicetext: unclosed markup in a template: %v", open))
	}
	sort.SliceStable(entities, func(i, j int) bool { return entities[i].GetOffset() < entities[j].GetOffset() })
	return text.String(), entities
}

// The login code ("app" is the name of the app), as Telegram sent it in
// April (Russian) and October (English) 2026, with one addition: the code
// is under a spoiler. Telegram's message has none; its clients hide the
// code themselves where they show it, Android everywhere
// (MessageObject.spoilLoginCode), iOS and Web K only in the chat list
// (ChatListItem.swift, messageForReply.ts), so in Web K the open chat showed
// it to anyone looking at the screen.
var loginCode = map[Lang]string{
	English: "**Login code:** ||{code}||. Do **not** give this code to anyone, even if they say they are from {app}!\n\n" +
		"❗️This code can be used to log in to your {app} account. We never ask it for anything else.\n\n" +
		"If you didn't request this code by trying to log in on another device, simply ignore this message.",
	// The space after "ещё." is Telegram's.
	Russian: "**Код для входа в {app}:** ||{code}||. Не давайте код **никому**, даже если его требуют от имени {app}!\n\n" +
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

// The message on a change of the cloud password or its recovery email, as
// Telegram sent it in English on 2026-05-11 (docs/tasks/019, the export);
// its Russian text is not known.
var twoStepChanged = map[Lang]string{
	English: "**Two-Step Verification settings changed.** {name}, your Two-Step Verification password and/or email were changed on {date} at {time} UTC.\n\n" +
		"Device: {device}\nLocation: {location}\n\n" +
		"If you didn’t request this, review your Active Sessions: **Settings > Devices** (or **Privacy & Security > Active Sessions**).",
}

// TwoStepChanged is a change of the account's cloud password or its
// recovery email, made from a session.
type TwoStepChanged struct {
	Name     string    // of the account
	Date     time.Time // of the change
	Device   string    // of the session that made it, as in NewLogin
	Location string
}

// Text is the message in lang.
func (c TwoStepChanged) Text(lang Lang) (string, []*mtproto.MessageEntity) {
	date := c.Date.UTC()
	return Render(textOf(twoStepChanged, lang), map[string]string{
		"name":     c.Name,
		"date":     date.Format("02/01/2006"),
		"time":     date.Format("15:04:05"),
		"device":   c.Device,
		"location": c.Location,
	})
}

// The messages about a restriction of the account (task 025). Telegram's
// own wording is not known: restrictions are reported through @SpamBot,
// which Pluma does not have.
var (
	restrictedUntil = map[Lang]string{
		English: "**Your account is limited** until {date} UTC after reports from other users. Until then you can only write to people who have you in their contacts, and you cannot write in groups and channels.",
		Russian: "**Ваш аккаунт ограничен** до {date} UTC после жалоб других пользователей. До этого времени Вы можете писать только тем, у кого Вы есть в контактах, и не можете писать в группах и каналах.",
	}
	restrictedForever = map[Lang]string{
		English: "**Your account is limited** after reports from other users. You can only write to people who have you in their contacts, and you cannot write in groups and channels.",
		Russian: "**Ваш аккаунт ограничен** после жалоб других пользователей. Вы можете писать только тем, у кого Вы есть в контактах, и не можете писать в группах и каналах.",
	}
	bannedUntil = map[Lang]string{
		English: "**Your account is blocked** until {date} UTC after reports from other users. You will not be able to log in until then.",
		Russian: "**Ваш аккаунт заблокирован** до {date} UTC после жалоб других пользователей. До этого времени войти в него нельзя.",
	}
	bannedForever = map[Lang]string{
		English: "**Your account is blocked** after reports from other users. You will not be able to log in.",
		Russian: "**Ваш аккаунт заблокирован** после жалоб других пользователей. Войти в него больше нельзя.",
	}
)

// Restricted is the message about a limit or, with banned, a ban of the
// account; a zero until has no end.
func Restricted(lang Lang, banned bool, until time.Time) (string, []*mtproto.MessageEntity) {
	tpl := restrictedForever
	switch {
	case banned && until.IsZero():
		tpl = bannedForever
	case banned:
		tpl = bannedUntil
	case !until.IsZero():
		tpl = restrictedUntil
	}
	return Render(textOf(tpl, lang), map[string]string{"date": until.UTC().Format("02/01/2006 15:04")})
}

func textOf(texts map[Lang]string, lang Lang) string {
	if t, ok := texts[lang]; ok {
		return t
	}
	return texts[English]
}
