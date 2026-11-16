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
	_ "embed"
	"fmt"
)

// The descriptions of the built-in inline bots (task 099, ADR 0062, p. 6),
// bot_info.description in the language of a session: what each bot does
// and where its results come from. They are in every language of own.json
// (the test holds them to it). The keys are inlinebots.Bot.About.
//
// inlinebots.json has mail.json's shape: {key: {lang_code: value}}.
//
//go:embed inlinebots.json
var inlineBotsJSON []byte

var inlineBotTexts = mustLoadTexts("inlinebots.json", inlineBotsJSON)

// InlineBotText returns the text of key in lang, with MailText's fallbacks.
// It panics on a key inlinebots.json does not have: the keys are constants of
// the code.
func InlineBotText(key, lang string) string {
	texts, ok := inlineBotTexts[key]
	if !ok {
		panic(fmt.Sprintf("langpack: no inline bot text %q", key))
	}
	return pick(texts, lang)
}
