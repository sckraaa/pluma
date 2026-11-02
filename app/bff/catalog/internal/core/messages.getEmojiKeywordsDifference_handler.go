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

// MessagesGetEmojiKeywordsDifference
// messages.getEmojiKeywordsDifference#1508b6af lang_code:string from_version:int = EmojiKeywordsDifference;
//
// The words of a language since from_version. The catalog keeps the words
// of its version only: a client at that version gets none, any other gets
// all of them, which it adds over what it has (emojiKeyword replaces a
// word's emoji).
func (c *CatalogCore) MessagesGetEmojiKeywordsDifference(in *mtproto.TLMessagesGetEmojiKeywordsDifference) (*mtproto.EmojiKeywordsDifference, error) {
	answer := &mtproto.EmojiKeywordsDifference{}
	_, found, err := c.stored("messages.getEmojiKeywords", in.GetLangCode(), answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return noKeywords(in.GetLangCode(), in.GetFromVersion()), nil
	}
	if answer.GetVersion() == in.GetFromVersion() {
		return noKeywords(in.GetLangCode(), answer.GetVersion()), nil
	}
	answer.LangCode = in.GetLangCode()
	answer.FromVersion = in.GetFromVersion()
	return answer, nil
}
