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

package notify

import (
	"strconv"
	"strings"
)

// texts are the strings of the iOS notifications in one language. {1},
// {2}, {3} are the author, the group and the member, as in the Telegram
// templates.
type texts struct {
	newMessage   string
	album        string
	forwarded    string // {1}: the count
	pinned       string // {1}: author, {2}: what
	pinnedNoText string
	contents     map[Content]string // {1}: the text
	actions      map[Action]string
}

var russian = &texts{
	newMessage:   "Новое сообщение",
	album:        "🖼 Альбом",
	forwarded:    "Пересланные сообщения: {1}",
	pinned:       "{1} закрепил(а) «{2}»",
	pinnedNoText: "{1} закрепил(а) сообщение",
	contents: map[Content]string{
		ContentNoText:  "Сообщение",
		ContentPhoto:   "📷 Фото",
		ContentVideo:   "📹 Видео",
		ContentRound:   "📹 Видеосообщение",
		ContentDoc:     "📎 Файл",
		ContentAudio:   "🎤 Голосовое сообщение",
		ContentGif:     "GIF",
		ContentGeo:     "📍 Геопозиция",
		ContentGeoLive: "📍 Трансляция геопозиции",
		ContentSticker: "{1} Стикер",
		ContentContact: "👤 Контакт: {1}",
		ContentPoll:    "📊 {1}",
		ContentQuiz:    "❓ {1}",
	},
	actions: map[Action]string{
		ActionCreated:      "{1} пригласил(а) вас в группу",
		ActionAddYou:       "{1} пригласил(а) вас в группу",
		ActionAddMember:    "{1} добавил(а) {3}",
		ActionDeleteYou:    "{1} удалил(а) вас из группы",
		ActionDeleteMember: "{1} удалил(а) {3}",
		ActionLeft:         "{1} покинул(а) группу",
		ActionReturned:     "{1} вернулся(-ась) в группу",
		ActionJoined:       "{1} присоединился(-ась) к группе",
		ActionTitleEdited:  "{1} изменил(а) название группы на «{2}»",
		ActionPhotoEdited:  "{1} изменил(а) фото группы",
	},
}

var english = &texts{
	newMessage:   "New message",
	album:        "🖼 Album",
	forwarded:    "Forwarded messages: {1}",
	pinned:       "{1} pinned «{2}»",
	pinnedNoText: "{1} pinned a message",
	contents: map[Content]string{
		ContentNoText:  "Message",
		ContentPhoto:   "📷 Photo",
		ContentVideo:   "📹 Video",
		ContentRound:   "📹 Video message",
		ContentDoc:     "📎 File",
		ContentAudio:   "🎤 Voice message",
		ContentGif:     "GIF",
		ContentGeo:     "📍 Location",
		ContentGeoLive: "📍 Live location",
		ContentSticker: "{1} Sticker",
		ContentContact: "👤 Contact: {1}",
		ContentPoll:    "📊 {1}",
		ContentQuiz:    "❓ {1}",
	},
	actions: map[Action]string{
		ActionCreated:      "{1} invited you to the group",
		ActionAddYou:       "{1} invited you to the group",
		ActionAddMember:    "{1} added {3}",
		ActionDeleteYou:    "{1} removed you from the group",
		ActionDeleteMember: "{1} removed {3}",
		ActionLeft:         "{1} left the group",
		ActionReturned:     "{1} returned to the group",
		ActionJoined:       "{1} joined the group",
		ActionTitleEdited:  "{1} renamed the group to «{2}»",
		ActionPhotoEdited:  "{1} changed the group photo",
	},
}

// textsFor returns the texts of a lang_code: Russian for ru, English for
// the rest.
func textsFor(lang string) *texts {
	if lang == "ru" || strings.HasPrefix(lang, "ru-") || strings.HasPrefix(lang, "ru_") {
		return russian
	}
	return english
}

// content is the body of a message with content c and text: the text
// itself for a text message, a label with it for the rest.
func (t *texts) content(c Content, text string) string {
	if c == ContentText {
		return text
	}
	if c == ContentSticker && text == "" {
		return strings.TrimSpace(format(t.contents[c], ""))
	}
	if s, ok := t.contents[c]; ok {
		if text != "" && !strings.Contains(s, "{1}") && (c == ContentPhoto || c == ContentVideo || c == ContentDoc) {
			return s + ", " + text // a caption
		}
		return format(s, text)
	}
	return t.contents[ContentNoText]
}

// format puts args into the {N} of s.
func format(s string, args ...string) string {
	for i, a := range args {
		s = strings.ReplaceAll(s, "{"+strconv.Itoa(i+1)+"}", a)
	}
	return s
}
