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

// MessagesGetEmojiKeywords
// messages.getEmojiKeywords#35a0e062 lang_code:string = EmojiKeywordsDifference;
//
// The emoji suggested for words of a language, from the catalog; a
// language it has none of answers no words at version 0, with lang_code as
// asked, as clients key their store by it.
func (c *CatalogCore) MessagesGetEmojiKeywords(in *mtproto.TLMessagesGetEmojiKeywords) (*mtproto.EmojiKeywordsDifference, error) {
	answer := &mtproto.EmojiKeywordsDifference{}
	_, found, err := c.stored("messages.getEmojiKeywords", in.GetLangCode(), answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return noKeywords(in.GetLangCode(), 0), nil
	}
	answer.LangCode = in.GetLangCode()
	answer.FromVersion = 0
	return answer, nil
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
