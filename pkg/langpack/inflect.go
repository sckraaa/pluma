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
	"unicode"
	"unicode/utf8"
)

// Inflected forms of "Telegram" become the same form of "Pluma". Telegram
// is a masculine noun ending in a consonant, Pluma a feminine one ending in
// "a", so the endings change with the name: Czech "v Telegramu" is
// "v Plumě", Hungarian "Telegramon" is "Plumán", Turkish "Telegram'ın" is
// "Pluma'nın". The forms are those of Telegram's packs of 2026-11-01 (every
// ending after the name in every pack); an ending a language does not list
// stays and Leftovers reports it.
//
// An inflector gets the ending after "Telegram" (with the apostrophe or
// hyphen that joins it, if any) and the word before the name, lower-cased,
// and returns the whole word that replaces "Telegram" and the ending.

type inflector func(ending, prev string) (string, bool)

var inflections = map[string]inflector{
	"cs": czech,
	"sk": slovak,
	"pl": endings(map[string]string{
		"a":   "Plumy",  // genitive
		"owi": "Plumie", // dative
		"ie":  "Plumie", // locative
		"em":  "Plumą",  // instrumental
	}),
	"hr": croatian,
	"sr": serbian,
	"fi": finnish,
	"hu": endings(map[string]string{
		"ot":  "Plumát",
		"nak": "Plumának",
		"on":  "Plumán",
		"ban": "Plumában",
		"ba":  "Plumába",
		"ból": "Plumából",
		"ra":  "Plumára",
		"ról": "Plumáról",
		"hoz": "Plumához",
		"tól": "Plumától",
		"nál": "Plumánál",
		"mal": "Plumával",
		"os":  "Plumás",
	}),
	"uz": uzbek,
	"kk": kazakh,
	"tr": turkish,
	"sv": compounds(false, setOf("met", "men", "mets", "mens")),
	"nb": compounds(false, setOf("met", "mer", "mene", "mets")),
	"de": compounds(true, setOf("m", "me", "men", "ms", "mes")),
	"ms": endings(map[string]string{"nya": "Plumanya"}),
}

// endings replaces the endings of a table.
func endings(table map[string]string) inflector {
	return func(ending, _ string) (string, bool) {
		w, ok := table[ending]
		return w, ok
	}
}

// czech: Telegramu is the genitive, the dative and the locative; the
// preposition or verb before it tells which. Pluma: Plumy, Plumě, Plumou.
func czech(ending, prev string) (string, bool) {
	switch ending {
	case "u":
		if czechDativeLocative[prev] {
			return "Plumě", true
		}
		return "Plumy", true
	case "em":
		return "Plumou", true
	}
	return "", false
}

// czechDativeLocative are the words before "Telegramu" in the packs that
// take the dative or the locative: prepositions and verbs of permitting
// and helping.
var czechDativeLocative = setOf("v", "ve", "na", "o", "po", "při", "k", "ke", "díky", "kvůli", "proti",
	"vůči", "povolte", "povolit", "dovolte", "dovolit", "umožnil", "umožnila", "umožněte", "umožní",
	"pomozte", "pomáhají", "pomoci")

// slovak: Telegramu is the genitive and the dative (the locative is
// Telegrame). Pluma: Plumy, Plume, Plumou; the adjective Telegramový is
// Plumový.
func slovak(ending, prev string) (string, bool) {
	switch ending {
	case "u":
		if slovakDative[prev] {
			return "Plume", true
		}
		return "Plumy", true
	case "e":
		return "Plume", true
	case "om":
		return "Plumou", true
	}
	if strings.HasPrefix(ending, "ov") {
		return "Plum" + ending, true
	}
	return "", false
}

var slovakDative = setOf("k", "ku", "vďaka", "kvôli", "voči", "proti", "oproti", "povoľte", "povoliť",
	"umožnil", "umožnila", "umožnite", "umožní", "pomôžte", "pomôcť", "prístup")

// croatian: Telegrama, Telegramu, Telegramom are Plume, Plumi, Plumom; the
// possessive Telegramov- is Plumin-.
func croatian(ending, _ string) (string, bool) {
	switch ending {
	case "a":
		return "Plume", true
	case "u":
		return "Plumi", true
	case "om":
		return "Plumom", true
	}
	if rest, ok := strings.CutPrefix(ending, "ov"); ok {
		return "Plumin" + rest, true
	}
	return "", false
}

// serbian: the Cyrillic packs join the endings with a hyphen
// (Telegram-у); the name stays in Latin letters, so the ending of Pluma
// does too.
func serbian(ending, _ string) (string, bool) {
	switch ending {
	case "-а":
		return "Plume", true
	case "-у":
		return "Plumi", true
	case "-ом":
		return "Plumom", true
	}
	if rest, ok := strings.CutPrefix(ending, "-ов"); ok {
		if latin, ok := serbianLatin(rest); ok {
			return "Plumin" + latin, true
		}
	}
	return "", false
}

var serbianLetters = map[rune]string{'а': "a", 'е': "e", 'и': "i", 'о': "o", 'у': "u", 'ј': "j", 'м': "m", 'г': "g", 'х': "h"}

func serbianLatin(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		l, ok := serbianLetters[r]
		if !ok {
			return "", false
		}
		b.WriteString(l)
	}
	return b.String(), true
}

// finnish: the endings of Telegram take the linking vowel i (Telegramissa,
// Telegramin), Pluma's go on its own a (Plumassa, Pluman); the illative
// Telegramiin is Plumaan, the partitive Telegramia Plumaa.
func finnish(ending, _ string) (string, bool) {
	rest, ok := strings.CutPrefix(ending, "i")
	if !ok || rest == "" || !isLower(rest) {
		return "", false
	}
	if r, ok := strings.CutPrefix(rest, "in"); ok {
		return "Plumaan" + r, true
	}
	return "Pluma" + rest, true
}

// uzbek: the case endings are the same after a vowel; the possessive
// drops its i (Telegramingiz, Plumangiz).
func uzbek(ending, _ string) (string, bool) {
	switch ending {
	case "ga", "da", "dagi", "dan", "ni", "ning":
		return "Pluma" + ending, true
	case "ingiz", "imiz", "im", "ing":
		return "Pluma" + ending[1:], true
	}
	return "", false
}

// kazakh: the endings follow a hyphen in Cyrillic; after a vowel the
// accusative is -ны, the ablative -дан, the possessive -сы.
func kazakh(ending, _ string) (string, bool) {
	switch ending {
	case "-ға", "-да", "-дағы", "-ның", "-мен", "'дағылардың":
		return "Pluma" + ending, true
	case "-ды":
		return "Pluma-ны", true
	case "-нан":
		return "Pluma-дан", true
	case "-ы":
		return "Pluma-сы", true
	case "-ыңызда":
		return "Pluma-ңызда", true
	}
	return "", false
}

// turkish: after a vowel the genitive is -nın and the accusative and the
// dative take a y (Pluma'nın, Pluma'yı, Pluma'ya); the apostrophe is kept,
// and added where the pack has none.
func turkish(ending, _ string) (string, bool) {
	mark := "'"
	if r, n := utf8.DecodeRuneInString(ending); r == '\'' || r == '’' {
		mark, ending = string(r), ending[n:]
	}
	switch ending {
	case "da", "daki", "dan":
		return "Pluma" + mark + ending, true
	case "ın":
		return "Pluma" + mark + "nın", true
	case "ı", "a":
		return "Pluma" + mark + "y" + ending, true
	}
	return "", false
}

// compounds: the genitive s stays (Plumas); a compound is hyphenated
// (Telegramkonto, Pluma-konto), its second part capitalized where nouns are
// (German Telegramnutzer, Pluma-Nutzer). The word telegram itself
// (German Telegramm, Swedish telegrammet) is no compound: its endings are
// in telegram.
func compounds(capital bool, telegram map[string]bool) inflector {
	return func(ending, _ string) (string, bool) {
		if ending == "s" {
			return brandName + "s", true
		}
		if !isLower(ending) || telegram[ending] {
			return "", false
		}
		if capital {
			r, n := utf8.DecodeRuneInString(ending)
			ending = string(unicode.ToUpper(r)) + ending[n:]
		}
		return brandName + "-" + ending, true
	}
}

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func isLower(s string) bool {
	for _, r := range s {
		if !unicode.IsLower(r) {
			return false
		}
	}
	return s != ""
}

// lowerCompounds are the languages that also write the name in lower case
// in compounds (Swedish telegramkonto, Slovak telegramový účet); the result
// is capitalized, as the name is elsewhere.
var lowerCompounds = setOf("sv", "nb", "sk")

// inflect replaces the inflected forms of "Telegram" in value for language
// lang, "TELEGRAM" in capitals too; the bare name is left to the word rule.
func inflect(lang, value string) string {
	lang = primaryLang(lang)
	in := inflections[lang]
	if in == nil {
		return value
	}
	caser := unicode.SpecialCase(nil)
	if lang == "tr" {
		caser = unicode.TurkishCase
	}
	var b strings.Builder
	pos := 0
	for i := 0; ; {
		j := indexName(value, i)
		if j < 0 {
			break
		}
		name := value[j : j+len("Telegram")]
		end := j + len(name)
		i = end
		if !wordBoundaryBefore(value, j) {
			continue
		}
		ending := endingAt(value, end)
		if ending == "" {
			continue
		}
		var word string
		ok := false
		switch {
		case name == "Telegram", name == "telegram" && lowerCompounds[lang]:
			word, ok = in(ending, previousWord(value, j))
		case name == "TELEGRAM" && strings.ToUpperSpecial(caser, ending) == ending:
			if word, ok = in(strings.ToLowerSpecial(caser, ending), previousWord(value, j)); ok {
				word = strings.ToUpperSpecial(caser, word)
			}
		}
		if !ok {
			continue
		}
		b.WriteString(value[pos:j])
		b.WriteString(word)
		pos = end + len(ending)
		i = pos
	}
	if pos == 0 {
		return value
	}
	b.WriteString(value[pos:])
	return b.String()
}

// indexName returns the index of the next "Telegram" at or after i in any
// case of its ASCII letters, or -1.
func indexName(value string, i int) int {
	for ; i+len("Telegram") <= len(value); i++ {
		if strings.EqualFold(value[i:i+len("Telegram")], "Telegram") {
			return i
		}
	}
	return -1
}

// endingAt returns the letters that continue the name at i, with the
// apostrophe or hyphen that joins them; "" if the name ends there.
func endingAt(value string, i int) string {
	j := i
	if r, n := utf8.DecodeRuneInString(value[j:]); r == '\'' || r == '’' || r == '-' {
		j += n
	}
	k := j
	for k < len(value) {
		r, n := utf8.DecodeRuneInString(value[k:])
		if !unicode.IsLetter(r) {
			break
		}
		k += n
	}
	if k == j {
		return ""
	}
	return value[i:k]
}

// previousWord returns the word before i, lower-cased, past spaces and
// markup (**, [).
func previousWord(value string, i int) string {
	end := i
	for end > 0 {
		r, n := utf8.DecodeLastRuneInString(value[:end])
		if unicode.IsLetter(r) {
			break
		}
		if r == ',' || r == '.' || r == '\n' {
			return ""
		}
		end -= n
	}
	start := end
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(value[:start])
		if !unicode.IsLetter(r) {
			break
		}
		start -= n
	}
	return strings.ToLower(value[start:end])
}
