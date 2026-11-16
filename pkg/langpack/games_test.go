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
)

// The game's button has its text in each of the 37 languages of own.json,
// each with the game's title in it once.
func TestGameTextsHaveEveryLanguage(t *testing.T) {
	langs := ownLanguages()
	if len(gameTexts) != 1 {
		t.Errorf("games.json has %d keys, want 1", len(gameTexts))
	}
	texts := gameTexts[gamePlayKey]
	if len(texts) != len(langs) {
		t.Errorf("%s has %d languages, want %d", gamePlayKey, len(texts), len(langs))
	}
	for _, lang := range langs {
		v := texts[lang]
		if strings.Count(v, "{title}") != 1 || strings.TrimSpace(strings.ReplaceAll(v, "{title}", "")) == "" {
			t.Errorf("%s %s: %q", gamePlayKey, lang, v)
		}
	}
}

func TestGamePlayButton(t *testing.T) {
	for lang, want := range map[string]string{
		"en":    "Play Lumberjack",
		"ru-RU": "Играть в Lumberjack",
		"de":    "Lumberjack spielen",
		"xx":    "Play Lumberjack",
		"":      "Play Lumberjack",
	} {
		if got := GamePlayButton("Lumberjack", lang); got != want {
			t.Errorf("%q: %q, want %q", lang, got, want)
		}
	}
	// A title is put in as it is, braces and percent signs too.
	if got := GamePlayButton("100% {fun}", "en"); got != "Play 100% {fun}" {
		t.Errorf("%q", got)
	}
}
