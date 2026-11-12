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
	"sort"
	"strings"
	"testing"
)

// ownLanguages are the languages of own.json: the languages of the clients.
func ownLanguages() []string {
	set := map[string]bool{}
	for _, keys := range ownStrings {
		for _, texts := range keys {
			for lang := range texts {
				set[lang] = true
			}
		}
	}
	langs := make([]string, 0, len(set))
	for l := range set {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

func TestMailJSONHasEveryLanguage(t *testing.T) {
	langs := ownLanguages()
	if len(langs) != 37 {
		t.Fatalf("own.json has %d languages, want the 37 of ADR 0046", len(langs))
	}
	if len(mailTexts) == 0 {
		t.Fatal("mail.json is empty")
	}
	for key, texts := range mailTexts {
		for _, lang := range langs {
			if strings.TrimSpace(texts[lang]) == "" {
				t.Errorf("%s has no %s text", key, lang)
			}
		}
		if len(texts) != len(langs) {
			t.Errorf("%s has %d languages, want %d", key, len(texts), len(langs))
		}
	}
	for _, key := range []string{
		"PlumaMailConfirmSubject", "PlumaMailConfirmBody", "PlumaMailRecoverySubject",
		"PlumaMailRecoveryBody", "PlumaMailValidFor", "PlumaMailIgnore", "PlumaPasswordRecoveredMessage",
	} {
		if _, ok := mailTexts[key]; !ok {
			t.Errorf("mail.json has no %s", key)
		}
	}
}

// The message about a recovered password is a template of servicetext: the
// same placeholders and closed bold markers in every language.
func TestMailRecoveredMessageTemplates(t *testing.T) {
	for lang, tpl := range mailTexts["PlumaPasswordRecoveredMessage"] {
		for _, p := range []string{"{name}", "{date}", "{time}", "{device}", "{location}"} {
			if strings.Count(tpl, p) != 1 {
				t.Errorf("%s: %s %d times", lang, p, strings.Count(tpl, p))
			}
		}
		if strings.Count(tpl, "**")%2 != 0 || strings.Count(tpl, "{") != 5 || strings.Count(tpl, "}") != 5 {
			t.Errorf("%s: unbalanced markup in %q", lang, tpl)
		}
	}
	// The subjects name the product, the bodies are one line each.
	for _, key := range []string{"PlumaMailConfirmSubject", "PlumaMailRecoverySubject"} {
		for lang, v := range mailTexts[key] {
			if !strings.Contains(v, "Pluma") || strings.ContainsAny(v, "\r\n") {
				t.Errorf("%s %s: %q", key, lang, v)
			}
		}
	}
}

func TestMailTextFallsBack(t *testing.T) {
	en := mailTexts["PlumaMailValidFor"]["en"]
	ru := mailTexts["PlumaMailValidFor"]["ru"]
	ptBR := mailTexts["PlumaMailValidFor"]["pt-br"]
	zhHans := mailTexts["PlumaMailValidFor"]["zh-hans"]
	for lang, want := range map[string]string{
		"ru":    ru,
		"ru-RU": ru, // the base language
		"RU_ru": ru,
		"pt-BR": ptBR,
		"pt":    ptBR, // another variant of the base
		"zh-cn": zhHans,
		"en":    en,
		"xx":    en, // unknown: English
		"":      en,
	} {
		if got := MailText("PlumaMailValidFor", lang); got != want {
			t.Errorf("%q: %q, want %q", lang, got, want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("an unknown key did not panic")
		}
	}()
	MailText("NoSuchKey", "en")
}

func TestMailRejects(t *testing.T) {
	for name, data := range map[string]string{
		"not json":     `{`,
		"no english":   `{"K": {"ru": "x"}}`,
		"empty text":   `{"K": {"en": "x", "ru": ""}}`,
		"bad language": `{"K": {"en": "x", "pt_BR": "y"}}`,
	} {
		if _, err := loadMail([]byte(data)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestMailRTL(t *testing.T) {
	for lang, want := range map[string]bool{"ar": true, "fa": true, "he": true, "he-IL": true, "en": false, "ru": false, "": false} {
		if MailRTL(lang) != want {
			t.Errorf("%q: %v", lang, !want)
		}
	}
}
