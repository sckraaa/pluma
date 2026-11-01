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
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The replacement of "Telegram" with "Pluma" in the values of the strings
// follows the client patches (ADR 0025): whole words only, never keys,
// addresses or handles.
//
//   - The word rule of the Android patch (pluma-android ci/patch.py,
//     TELEGRAM_WORD): (?<![A-Za-z0-9@/.])(Telegram|TELEGRAM)(?![A-Za-z0-9]|\.\w)
//     gives "Pluma" and "PLUMA"; "t.me/TelegramTips", "@Telegram",
//     "Telegram.org" and "Telegramm" stay.
//   - "Telegram Desktop" is "Pluma", as in the Desktop patch
//     (pluma-desktop ci/patch.py, name_telegram).
//   - The name in other scripts and in compounds, per language and longest
//     first (Android STRING_LOCAL_NAMES; Persian from the iOS patch,
//     INFOPLIST_NAMES). Korean particles follow the last sound: "Pluma" ends
//     in a vowel.
//   - The links of the QR login to the desktop and web clients lead to
//     Pluma's site and web client (Android CLIENT_LINKS), the one exception
//     to keeping telegram.org.
//   - Keys whose text names Telegram's API itself keep it (Desktop
//     LANG_KEEP: lng_about_text1_api).
//
// Inflected forms ("Telegramu" in Czech) are not words of the rule and stay;
// Leftovers reports them.

const brandName = "Pluma"

// keepKeys are the keys whose values stay as Telegram wrote them.
var keepKeys = map[string]bool{
	"lng_about_text1_api": true, // "Telegram API", the protocol Pluma speaks
}

// isKept reports whether the value of key stays as Telegram wrote it.
func isKept(key string) bool { return keepKeys[key] }

// clientLinkKeys are the keys of the QR login's links (Android).
var clientLinkKeys = map[string]bool{
	"AuthAnotherClientDownloadClientUrl": true,
	"AuthAnotherWebClientUrl":            true,
	"AuthAnotherClientInfo1":             true,
	"AuthAnotherClientInfo5":             true,
}

// clientLinks are the replacements in clientLinkKeys, in this order.
var clientLinks = strings.NewReplacer(
	"https://getdesktop.telegram.org/", "https://pluma.chat/",
	"https://desktop.telegram.org/", "https://pluma.chat/",
	"https://desktop.telegram.org", "https://pluma.chat/",
	"https://web.telegram.org/", "https://web.pluma.chat/",
	"getdesktop.telegram.org", "pluma.chat",
	"desktop.telegram.org", "pluma.chat",
	"web.telegram.org", "web.pluma.chat",
)

// sentenceRewrites are sentences that would turn false with the name
// changed (Android STRING_REWRITES).
var sentenceRewrites = strings.NewReplacer(
	"e.g., all videos that were sent from Android using Telegram 4.7 and earlier versions.",
	"e.g., videos that were sent from very old versions of the app.",
)

// localNames are the name in other scripts and in compounds by language
// code, longest first.
var localNames = map[string]*strings.Replacer{
	"ar": strings.NewReplacer(
		"لتيليجرام", "لـPluma",
		"تيليجرام", "Pluma",
		"تليجرام", "Pluma",
	),
	"fa": strings.NewReplacer(
		"تلگرام", "Pluma",
	),
	"ko": strings.NewReplacer(
		"텔레그램이라는", "Pluma라는",
		"텔레그램이", "Pluma가",
		"텔레그램으로", "Pluma로",
		"텔레그램은", "Pluma는",
		"텔레그램을", "Pluma를",
		"텔레그램", "Pluma",
	),
	"nl": strings.NewReplacer(
		"Telegramberichten", "Pluma-berichten",
	),
}

// Rebrand returns s with "Telegram" replaced in its texts for language
// lang. Keys never change.
func Rebrand(lang string, s String) String {
	r := s
	r.Value = RebrandValue(lang, s.Key, s.Value)
	for i, f := range r.forms() {
		if v := *s.forms()[i]; v != nil {
			nv := RebrandValue(lang, s.Key, *v)
			*f = &nv
		}
	}
	return r
}

// RebrandValue returns the text value of key in language lang with
// "Telegram" replaced.
func RebrandValue(lang, key, value string) string {
	if isKept(key) {
		return value
	}
	if clientLinkKeys[key] {
		value = clientLinks.Replace(value)
	}
	value = sentenceRewrites.Replace(value)
	if r := localNames[primaryLang(lang)]; r != nil {
		value = r.Replace(value)
	}
	return replaceWord(value)
}

// primaryLang returns the language of a code without its region or variant
// ("pt-br" -> "pt").
func primaryLang(code string) string {
	if i := strings.IndexByte(code, '-'); i >= 0 {
		return code[:i]
	}
	return code
}

// replaceWord applies the word rule, with "Telegram Desktop" as one name.
func replaceWord(value string) string {
	var b strings.Builder
	pos := 0
	for i := 0; i < len(value); {
		j := nextName(value, i)
		if j < 0 {
			break
		}
		word := value[j : j+len("Telegram")]
		end := j + len(word)
		if !wordBoundaryBefore(value, j) || !wordBoundaryAfter(value, end) {
			i = j + 1
			continue
		}
		repl := brandName
		if word == "TELEGRAM" {
			repl = strings.ToUpper(brandName)
		} else if rest := value[end:]; strings.HasPrefix(rest, " Desktop") && wordBoundaryAfter(value, end+len(" Desktop")) {
			end += len(" Desktop")
		}
		b.WriteString(value[pos:j])
		b.WriteString(repl)
		pos, i = end, end
	}
	if pos == 0 {
		return value
	}
	b.WriteString(value[pos:])
	return b.String()
}

// nextName returns the index of the next "Telegram" or "TELEGRAM" at or
// after i, or -1.
func nextName(value string, i int) int {
	a := strings.Index(value[i:], "Telegram")
	c := strings.Index(value[i:], "TELEGRAM")
	switch {
	case a < 0 && c < 0:
		return -1
	case a < 0 || (c >= 0 && c < a):
		return i + c
	default:
		return i + a
	}
}

// wordBoundaryBefore: (?<![A-Za-z0-9@/.]) at i.
func wordBoundaryBefore(value string, i int) bool {
	if i == 0 {
		return true
	}
	c := value[i-1]
	return !isASCIIAlnum(c) && c != '@' && c != '/' && c != '.'
}

// wordBoundaryAfter: (?![A-Za-z0-9]|\.\w) at i, \w being a Unicode word
// character as in Python's re.
func wordBoundaryAfter(value string, i int) bool {
	if i >= len(value) {
		return true
	}
	c := value[i]
	if isASCIIAlnum(c) {
		return false
	}
	if c == '.' && i+1 < len(value) {
		r, _ := utf8.DecodeRuneInString(value[i+1:])
		if isWordRune(r) {
			return false
		}
	}
	return true
}

func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

// leftoverName finds the name of Telegram in the scripts of its packs.
var leftoverName = regexp.MustCompile(`(?i)telegram|телеграм|텔레그램|تيليجرام|تليجرام|تلگرام`)

// Leftovers returns the words that still name Telegram in value (such as
// "Telegramu"), outside addresses and handles: what the rules leave and an
// administrator should look at.
func Leftovers(value string) []string {
	var words []string
	for _, m := range leftoverName.FindAllStringIndex(value, -1) {
		if inAddress(value, m[0], m[1]) {
			continue
		}
		start, end := m[0], m[1]
		for start > 0 {
			r, n := utf8.DecodeLastRuneInString(value[:start])
			if !isWordRune(r) {
				break
			}
			start -= n
		}
		for end < len(value) {
			r, n := utf8.DecodeRuneInString(value[end:])
			if !isWordRune(r) {
				break
			}
			end += n
		}
		if w := value[start:end]; !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	return words
}

// inAddress reports whether value[i:j] is part of an address or a handle:
// its token (between spaces and brackets) has a scheme, a '/', an '@' or a
// '.' right before it, or the name is followed by ".word" (telegram.org).
func inAddress(value string, i, j int) bool {
	start := i
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(value[:start])
		if unicode.IsSpace(r) || strings.ContainsRune(`"'()[]<>*`, r) {
			break
		}
		start -= n
	}
	end := j
	for end < len(value) {
		r, n := utf8.DecodeRuneInString(value[end:])
		if unicode.IsSpace(r) || strings.ContainsRune(`"'()[]<>*`, r) {
			break
		}
		end += n
	}
	token := value[start:end]
	if strings.Contains(token, "://") || strings.HasPrefix(token, "@") {
		return true
	}
	if i > start && strings.ContainsRune("/@.", rune(value[i-1])) {
		return true
	}
	return !wordBoundaryAfter(value, j) && value[j] == '.'
}
