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
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// Every bot of the registry has its description in each of the 42 languages
// of the packs, within the limit of a bot's description, with the bot's
// username.
func TestInlineBotTextsHaveEveryLanguage(t *testing.T) {
	langs := allLanguages()
	if len(langs) != 42 {
		t.Fatalf("%d languages, want the 42 of the packs", len(langs))
	}
	if len(inlineBotTexts) != len(inlinebots.All()) {
		t.Errorf("inlinebots.json has %d keys, the registry %d bots", len(inlineBotTexts), len(inlinebots.All()))
	}
	for _, b := range inlinebots.All() {
		texts, ok := inlineBotTexts[b.About]
		if !ok {
			t.Errorf("@%s: no %s", b.Username, b.About)
			continue
		}
		if len(texts) != len(langs) {
			t.Errorf("%s has %d languages, want %d", b.About, len(texts), len(langs))
		}
		for _, lang := range langs {
			v := texts[lang]
			switch {
			case strings.TrimSpace(v) == "":
				t.Errorf("%s has no %s text", b.About, lang)
			case utf8.RuneCountInString(v) > 512: // botstore.MaxDescription
				t.Errorf("%s %s: %d characters", b.About, lang, utf8.RuneCountInString(v))
			case !strings.Contains(v, "@"+b.Username):
				t.Errorf("%s %s does not name @%s: %q", b.About, lang, b.Username, v)
			}
		}
	}
}

func TestInlineBotTextFallsBack(t *testing.T) {
	key := inlinebots.Pic.About
	texts := inlineBotTexts[key]
	for lang, want := range map[string]string{
		"ru":    texts["ru"],
		"ru-RU": texts["ru"],
		"ar":    texts["ar"],
		"pt":    texts["pt-br"],
		"zh-tw": texts["zh-hans"], // another variant of the base, the first by name
		"xx":    texts["en"],
		"":      texts["en"],
	} {
		if got := InlineBotText(key, lang); got != want {
			t.Errorf("%q: %q, want %q", lang, got, want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("an unknown key did not panic")
		}
	}()
	InlineBotText("NoSuchKey", "en")
}

// loadTexts names the file it rejects.
func TestLoadTextsNamesTheFile(t *testing.T) {
	_, err := loadTexts("inlinebots.json", []byte(`{"K": {"ru": "x"}}`))
	if err == nil || !strings.Contains(err.Error(), "inlinebots.json") {
		t.Errorf("error %v", err)
	}
}
