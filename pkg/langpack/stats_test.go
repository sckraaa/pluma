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

// statsKeys are the keys the code names, as stats.go declares them.
var statsKeys = append([]string{
	StatsSubscribers, StatsSourceSubscribers, StatsMembers,
	StatsJoined, StatsLeft, StatsMuted, StatsUnmuted,
	StatsThisPeriod, StatsPreviousPeriod,
	StatsViews, StatsShares,
	StatsSourceChannels, StatsSourceGroups, StatsSourcePrivate, StatsSourceOther,
	StatsJoinSourceLink, StatsJoinSourceFolder, StatsJoinSourceRequest, StatsJoinSourcePublic, StatsJoinSourceAdded,
	StatsOtherLanguages,
	StatsReactionsPositive, StatsReactionsNegative, StatsReactionsOther,
	StatsMessagesText, StatsMessagesPhotos, StatsMessagesVideos, StatsMessagesVoice, StatsMessagesRound,
	StatsMessagesStickers, StatsMessagesGifs, StatsMessagesFiles, StatsMessagesMusic, StatsMessagesOther,
	StatsMessages, StatsViewers,
	StatsNotEnoughData, StatsNoVotes, StatsNoInstantView,
}, StatsWeekdays[:]...)

// Every key of stats.json has a text in each of the 37 languages of
// own.json and none other, without a "-" (tdesktop turns it into a dash in
// a series name), and every key is one the code names: a constant, a day of
// the week or the name of a language.
func TestStatsTextsHaveEveryLanguage(t *testing.T) {
	langs := textLanguages()
	if len(langs) != 37 {
		t.Fatalf("%d languages, want the 37 of ADR 0046", len(langs))
	}
	named := map[string]bool{}
	for _, key := range statsKeys {
		if named[key] {
			t.Errorf("%s is declared twice", key)
		}
		named[key] = true
		if _, ok := statsTexts[key]; !ok {
			t.Errorf("stats.json has no %s", key)
		}
	}
	for key, texts := range statsTexts {
		if !named[key] && !strings.HasPrefix(key, statsLanguagePrefix) {
			t.Errorf("%s is not a key the code names", key)
		}
		if len(texts) != len(langs) {
			t.Errorf("%s has %d languages, want %d", key, len(texts), len(langs))
		}
		for _, lang := range langs {
			v := texts[lang]
			switch {
			case strings.TrimSpace(v) != v || v == "":
				t.Errorf("%s has no %s text or spaces around it: %q", key, lang, v)
			case strings.Contains(v, "-"):
				t.Errorf("%s %s has a \"-\": %q", key, lang, v)
			case lang == "uz" && strings.Contains(v, "'"):
				// Uzbek writes oʻ and gʻ with "‘" and the tutuq belgisi
				// with "’", as own.json and mail.json do.
				t.Errorf("%s uz has a \"'\": %q", key, v)
			}
		}
	}
}

// The graph of languages names every language of the server's packs, by its
// base code, and only those.
func TestStatsLanguageNames(t *testing.T) {
	bases := map[string]bool{}
	for _, lang := range textLanguages() {
		base, _, _ := strings.Cut(lang, "-")
		bases[base] = true
		key, ok := StatsLanguageKey(lang)
		if !ok || key != statsLanguagePrefix+base {
			t.Errorf("%s: %q, %v", lang, key, ok)
		}
	}
	n := 0
	for key := range statsTexts {
		if code, ok := strings.CutPrefix(key, statsLanguagePrefix); ok {
			n++
			if !bases[code] {
				t.Errorf("%s names a language the server has no pack of", key)
			}
		}
	}
	if n != len(bases) {
		t.Errorf("%d names of languages, want %d", n, len(bases))
	}

	for code, want := range map[string]string{
		"ru":      "PlumaStatsLang_ru",
		"pt-BR":   "PlumaStatsLang_pt",
		"pt_br":   "PlumaStatsLang_pt",
		"zh-hant": "PlumaStatsLang_zh",
		" EN ":    "PlumaStatsLang_en",
	} {
		if got, ok := StatsLanguageKey(code); !ok || got != want {
			t.Errorf("%q: %q, %v, want %q", code, got, ok, want)
		}
	}
	for _, code := range []string{"", "ja", "xx-yy", "-"} {
		if got, ok := StatsLanguageKey(code); ok {
			t.Errorf("%q: %q, want none", code, got)
		}
	}
	if got := StatsText("PlumaStatsLang_ru", "ru"); got != "Русский" {
		t.Errorf("Russian in Russian: %q", got)
	}
	if got := StatsText("PlumaStatsLang_de", "en"); got != "German" {
		t.Errorf("German in English: %q", got)
	}
}

func TestStatsTextFallsBack(t *testing.T) {
	texts := statsTexts[StatsJoined]
	for lang, want := range map[string]string{
		"ru":    texts["ru"],
		"ru-RU": texts["ru"],
		"ar":    texts["ar"],
		"pt":    texts["pt-br"],
		"zh-tw": texts["zh-hans"], // another variant of the base, the first by name
		"xx":    texts["en"],
		"":      texts["en"],
	} {
		if got := StatsText(StatsJoined, lang); got != want {
			t.Errorf("%q: %q, want %q", lang, got, want)
		}
	}
	if got := StatsText(StatsWeekdays[0], "en"); got != "Monday" {
		t.Errorf("the first weekday is %q, want Monday", got)
	}
	if got := StatsText(StatsSubscribers, "ru"); got != "Подписчики" {
		t.Errorf("subscribers in Russian: %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("an unknown key did not panic")
		}
	}()
	StatsText("NoSuchKey", "en")
}
