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

// Pluma's own strings: texts of Pluma's client patches that Telegram's packs
// do not have (ADR 0046). The apps that bundle only English (iOS, tdesktop,
// Web K) take every other language from the server, so these strings join
// each import of their pack, in the language imported. They are written as
// they are: "Telegram" in them is the real Telegram, never replaced (task 076:
// the login code screens). A language without its own text gets the English
// one, listed in ImportResult.OwnMissing.
//
// own.json: {pack: {key: {lang_code: value}}}, every key with "en".
//
//go:embed own.json
var ownJSON []byte

var ownStrings = mustLoadOwn(ownJSON)

func mustLoadOwn(data []byte) map[string]map[string]map[string]string {
	own, err := loadOwn(data)
	if err != nil {
		panic(err)
	}
	return own
}

func loadOwn(data []byte) (map[string]map[string]map[string]string, error) {
	var own map[string]map[string]map[string]string
	if err := json.Unmarshal(data, &own); err != nil {
		return nil, fmt.Errorf("langpack: own.json: %w", err)
	}
	for pack, keys := range own {
		if CanonicalPack(pack) != pack {
			return nil, fmt.Errorf("langpack: own.json: pack %q is not lower case", pack)
		}
		for key, texts := range keys {
			if key == "" || texts["en"] == "" {
				return nil, fmt.Errorf("langpack: own.json: %s/%q has no English text", pack, key)
			}
			for lang, v := range texts {
				if CanonicalLangCode(lang) != lang || v == "" {
					return nil, fmt.Errorf("langpack: own.json: %s/%s: bad language %q or empty text", pack, key, lang)
				}
			}
		}
	}
	return own, nil
}

// ownFor returns Pluma's own strings of pack in lang, sorted by key, and the
// keys that fell back to English.
func ownFor(pack, lang string) (strs []String, missing []string) {
	keys := ownStrings[pack]
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	base, _, _ := strings.Cut(lang, "-")
	for _, k := range names {
		texts := keys[k]
		v, ok := texts[lang]
		if !ok {
			v, ok = texts[base]
		}
		if !ok {
			v = texts["en"]
			if lang != "en" {
				missing = append(missing, k)
			}
		}
		strs = append(strs, String{Key: k, Value: v})
	}
	return strs, missing
}
