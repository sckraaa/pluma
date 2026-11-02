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
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// MessagesGetEmojiKeywords
// messages.getEmojiKeywords#35a0e062 lang_code:string = EmojiKeywordsDifference;
//
// The emoji suggested for words of a language, from the catalog (or of
// its base language); a language it has none of answers no words at
// version 0, with lang_code as asked.
func (c *CatalogCore) MessagesGetEmojiKeywords(in *mtproto.TLMessagesGetEmojiKeywords) (*mtproto.EmojiKeywordsDifference, error) {
	answer, lang, err := c.keywords(in.GetLangCode())
	if err != nil {
		return nil, err
	}
	if answer == nil {
		return noKeywords(in.GetLangCode(), 0), nil
	}
	answer.LangCode = lang
	answer.FromVersion = 0
	return answer, nil
}

// keywords is the stored words of lang: of the language itself, or of its
// base language for a regional one ("en" for "en-US", which Android and iOS
// send from the keyboard), with the code they are stored under, which
// clients keep beside the one they asked for (Android
// emoji_keywords_info_v2.alias, iOS EmojiKeywordCollection).
func (c *CatalogCore) keywords(lang string) (*mtproto.EmojiKeywordsDifference, string, error) {
	for _, code := range langCandidates(lang) {
		answer := &mtproto.EmojiKeywordsDifference{}
		_, found, err := c.stored("messages.getEmojiKeywords", code, answer)
		if err != nil {
			return nil, "", err
		}
		if found {
			return answer, code, nil
		}
	}
	return nil, "", nil
}

// langCandidates are the codes lang is looked up by: itself, lower-cased,
// then its base language.
func langCandidates(lang string) []string {
	code := strings.ToLower(strings.ReplaceAll(lang, "_", "-"))
	out := []string{lang}
	if code != lang {
		out = append(out, code)
	}
	if base, _, ok := strings.Cut(code, "-"); ok && base != "" {
		out = append(out, base)
	}
	return out
}

// noKeywords is a difference of no words of lang at version.
func noKeywords(lang string, version int32) *mtproto.EmojiKeywordsDifference {
	return mtproto.MakeTLEmojiKeywordsDifference(&mtproto.EmojiKeywordsDifference{
		LangCode:    lang,
		FromVersion: version,
		Version:     version,
		Keywords:    []*mtproto.EmojiKeyword{},
	}).To_EmojiKeywordsDifference()
}
