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

package usersets

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The lengths of core.telegram.org/method/stickers.createStickerSet: a
// title and a short name of 1 to 64 characters.
const (
	MaxTitle     = 64
	MaxShortName = 64
)

// ValidTitle tells if title may name a set: 1 to 64 characters, not all
// of them spaces.
func ValidTitle(title string) bool {
	return strings.TrimSpace(title) != "" && utf8.RuneCountInString(title) <= MaxTitle
}

// ValidShortName tells if name may be the short name of a set, the one of
// its link (t.me/addstickers/<name>): 1 to 64 English letters, digits and
// underscores, beginning with a letter, without two underscores in a row
// (core.telegram.org/method/stickers.createStickerSet).
func ValidShortName(name string) bool {
	if name == "" || len(name) > MaxShortName || !isLetter(name[0]) || strings.Contains(name, "__") {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; !isLetter(c) && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// cyrillic is how the Russian letters of a title are written in a short
// name.
var cyrillic = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y",
	'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f",
	'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// ShortNameOf makes a short name of a title: its letters and digits (the
// Russian ones in Latin), the rest as single underscores; "" for a title
// that has no letter to begin with. suffix, if not empty, is added after
// an underscore, the name cut to make room for it.
func ShortNameOf(title, suffix string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r < utf8.RuneSelf && (isLetter(byte(r)) || (r >= '0' && r <= '9')):
			b.WriteRune(r)
		case cyrillic[unicode.ToLower(r)] != "" || unicode.ToLower(r) == 'ъ' || unicode.ToLower(r) == 'ь':
			t := cyrillic[unicode.ToLower(r)]
			if unicode.IsUpper(r) && t != "" {
				t = strings.ToUpper(t[:1]) + t[1:]
			}
			b.WriteString(t)
		default:
			b.WriteByte('_')
		}
	}
	// One underscore between words, none at the ends, a letter first.
	parts := strings.FieldsFunc(b.String(), func(r rune) bool { return r == '_' })
	name := strings.Join(parts, "_")
	for name != "" && !isLetter(name[0]) {
		name = strings.TrimLeft(name[1:], "_")
	}
	if name == "" {
		return ""
	}
	room := MaxShortName
	if suffix != "" {
		room -= len(suffix) + 1
	}
	if len(name) > room {
		name = strings.TrimRight(name[:room], "_")
	}
	if suffix != "" {
		name += "_" + suffix
	}
	return name
}
