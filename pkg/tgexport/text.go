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

package tgexport

import (
	"strings"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
)

// utf16Len is the length of s in UTF-16 code units, which the offsets of
// message entities count (core.telegram.org/api/entities).
func utf16Len(s string) int32 {
	n := int32(0)
	for _, r := range s {
		n += int32(utf16.RuneLen(r))
	}
	return n
}

// Entities turns the parts of a text into the text and its entities. user
// gives the Pluma account of a Telegram user a mention_name names; such a
// mention of a user it does not know stays plain text, and so does a custom
// emoji, whose text is the emoji it stands for and whose document the
// export does not name. The users mentioned by name are returned as well.
func Entities(parts []TextPart, user func(telegramId int64) (int64, bool)) (text string, entities []*mtproto.MessageEntity, mentioned []int64) {
	var b strings.Builder
	offset := int32(0)
	for _, p := range parts {
		length := utf16Len(p.Text)
		b.WriteString(p.Text)
		if length == 0 {
			continue
		}
		e := &mtproto.MessageEntity{Offset: offset, Length: length}
		offset += length
		var made *mtproto.MessageEntity
		switch p.Type {
		case "bold":
			made = mtproto.MakeTLMessageEntityBold(e).To_MessageEntity()
		case "italic":
			made = mtproto.MakeTLMessageEntityItalic(e).To_MessageEntity()
		case "underline":
			made = mtproto.MakeTLMessageEntityUnderline(e).To_MessageEntity()
		case "strikethrough":
			made = mtproto.MakeTLMessageEntityStrike(e).To_MessageEntity()
		case "spoiler":
			made = mtproto.MakeTLMessageEntitySpoiler(e).To_MessageEntity()
		case "code":
			made = mtproto.MakeTLMessageEntityCode(e).To_MessageEntity()
		case "pre":
			e.Language = p.Language
			made = mtproto.MakeTLMessageEntityPre(e).To_MessageEntity()
		case "blockquote":
			e.Collapsed = p.Collapsed
			made = mtproto.MakeTLMessageEntityBlockquote(e).To_MessageEntity()
		case "text_link":
			if p.Href == "" {
				continue
			}
			e.Url = p.Href
			made = mtproto.MakeTLMessageEntityTextUrl(e).To_MessageEntity()
		case "link":
			made = mtproto.MakeTLMessageEntityUrl(e).To_MessageEntity()
		case "email":
			made = mtproto.MakeTLMessageEntityEmail(e).To_MessageEntity()
		case "phone":
			made = mtproto.MakeTLMessageEntityPhone(e).To_MessageEntity()
		case "mention":
			made = mtproto.MakeTLMessageEntityMention(e).To_MessageEntity()
		case "hashtag":
			made = mtproto.MakeTLMessageEntityHashtag(e).To_MessageEntity()
		case "cashtag":
			made = mtproto.MakeTLMessageEntityCashtag(e).To_MessageEntity()
		case "bot_command":
			made = mtproto.MakeTLMessageEntityBotCommand(e).To_MessageEntity()
		case "bank_card":
			made = mtproto.MakeTLMessageEntityBankCard(e).To_MessageEntity()
		case "mention_name":
			id, ok := int64(0), false
			if user != nil && p.UserId != 0 {
				id, ok = user(p.UserId)
			}
			if !ok {
				continue
			}
			e.UserId_INT64 = id
			made = mtproto.MakeTLMessageEntityMentionName(e).To_MessageEntity()
			mentioned = append(mentioned, id)
		default:
			// plain, unknown, custom_emoji, ton_address and whatever a
			// later Telegram Desktop adds: the text alone.
			continue
		}
		entities = append(entities, made)
	}
	return b.String(), entities, mentioned
}
