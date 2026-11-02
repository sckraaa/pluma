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
	"github.com/teamgram/proto/mtproto"
)

// MessagesGetEmojiKeywordsLanguages
// messages.getEmojiKeywordsLanguages#4e9963b2 lang_codes:Vector<string> = Vector<EmojiLanguage>;
//
// The languages of lang_codes the catalog has words for, by the codes
// they are stored under ("en" for "en-US").
func (c *CatalogCore) MessagesGetEmojiKeywordsLanguages(in *mtproto.TLMessagesGetEmojiKeywordsLanguages) (*mtproto.Vector_EmojiLanguage, error) {
	langs := []*mtproto.EmojiLanguage{}
	seen := map[string]bool{}
	for i, code := range in.GetLangCodes() {
		if i >= maxLanguages {
			break
		}
		answer, lang, err := c.keywords(code)
		if err != nil {
			return nil, err
		}
		if answer != nil && !seen[lang] {
			seen[lang] = true
			langs = append(langs, mtproto.MakeTLEmojiLanguage(&mtproto.EmojiLanguage{LangCode: lang}).To_EmojiLanguage())
		}
	}
	return &mtproto.Vector_EmojiLanguage{Datas: langs}, nil
}

// maxLanguages bounds the languages one request looks up; clients send the
// few of their keyboards.
const maxLanguages = 32
