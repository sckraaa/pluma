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
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The texts the server writes itself in the language of a session: the
// emails with the codes of the recovery email of the cloud password and the
// message about a password recovered with one (task 091, ADR 0058, p. 9).
// They are in every language of own.json (the test holds them to it).
//
// mail.json: {key: {lang_code: value}}, every key with "en"; the keys of
// own.json's shape without the level of the pack.
//
//go:embed mail.json
var mailJSON []byte

var mailTexts = mustLoadMail(mailJSON)

func mustLoadMail(data []byte) map[string]map[string]string {
	m, err := loadMail(data)
	if err != nil {
		panic(err)
	}
	return m
}

func loadMail(data []byte) (map[string]map[string]string, error) {
	var m map[string]map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("langpack: mail.json: %w", err)
	}
	for key, texts := range m {
		if key == "" || texts["en"] == "" {
			return nil, fmt.Errorf("langpack: mail.json: %q has no English text", key)
		}
		for lang, v := range texts {
			if CanonicalLangCode(lang) != lang || v == "" {
				return nil, fmt.Errorf("langpack: mail.json: %s: bad language %q or empty text", key, lang)
			}
		}
	}
	return m, nil
}

// MailText returns the text of key in lang (a lang_code of initConnection,
// "pt-BR" as well as "pt-br"): the language's own, else the base language's
// ("pt" for "pt-br"), else that of another variant of the base language
// ("pt-br" for "pt", "zh-hans" for "zh-cn"), else the English one. It panics
// on a key mail.json does not have: the keys are constants of the code.
func MailText(key, lang string) string {
	texts, ok := mailTexts[key]
	if !ok {
		panic(fmt.Sprintf("langpack: no mail text %q", key))
	}
	return pick(texts, lang)
}

func pick(texts map[string]string, lang string) string {
	lang = CanonicalLangCode(lang)
	if v, ok := texts[lang]; ok {
		return v
	}
	base, _, _ := strings.Cut(lang, "-")
	if v, ok := texts[base]; ok {
		return v
	}
	if base != "" {
		variants := make([]string, 0, 2)
		for l := range texts {
			if b, _, _ := strings.Cut(l, "-"); b == base {
				variants = append(variants, l)
			}
		}
		if len(variants) > 0 {
			sort.Strings(variants)
			return texts[variants[0]]
		}
	}
	return texts["en"]
}

// MailRTL reports whether lang is written right to left (Arabic, Persian,
// Hebrew among the languages of mail.json), for the direction of an email.
func MailRTL(lang string) bool {
	base, _, _ := strings.Cut(CanonicalLangCode(lang), "-")
	switch base {
	case "ar", "fa", "he":
		return true
	}
	return false
}
