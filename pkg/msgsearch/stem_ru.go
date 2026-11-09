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

package msgsearch

import (
	"slices"
	"strings"
)

// The endings of the Russian stemmer of the Snowball project
// (snowballstem.org/algorithms/russian/stemmer.html). The endings of a
// group1 must follow "а" or "я".
var (
	gerund1 = []string{"вшись", "вши", "в"}
	gerund2 = []string{"ившись", "ывшись", "ивши", "ывши", "ив", "ыв"}

	reflexive = []string{"ся", "сь"}

	adjective = []string{
		"ее", "ие", "ые", "ое", "ими", "ыми", "ей", "ий", "ый", "ой", "ем", "им", "ым", "ом",
		"его", "ого", "ему", "ому", "их", "ых", "ую", "юю", "ая", "яя", "ою", "ею",
	}
	participle1 = []string{"ем", "нн", "вш", "ющ", "щ"}
	participle2 = []string{"ивш", "ывш", "ующ"}

	verb1 = []string{
		"ла", "на", "ете", "йте", "ли", "й", "л", "ем", "н", "ло", "но", "ет", "ют", "ны", "ть", "ешь", "нно",
	}
	verb2 = []string{
		"ила", "ыла", "ена", "ейте", "уйте", "ите", "или", "ыли", "ей", "уй", "ил", "ыл", "им", "ым", "ен",
		"ило", "ыло", "ено", "ят", "ует", "уют", "ит", "ыт", "ены", "ить", "ыть", "ишь", "ую", "ю",
	}

	noun = []string{
		"а", "ев", "ов", "ие", "ье", "е", "иями", "ями", "ами", "еи", "ии", "и", "ией", "ей", "ой", "ий",
		"й", "иям", "ям", "ием", "ем", "ам", "ом", "о", "у", "ах", "иях", "ях", "ы", "ь", "ию", "ью", "ю",
		"ия", "ья", "я",
	}

	superlative  = []string{"ейше", "ейш"}
	derivational = []string{"ость", "ост"}
	softSign     = []string{"ь"}
	finalI       = []string{"и"}
)

func init() {
	// The longest ending wins.
	for _, l := range [][]string{
		gerund1, gerund2, reflexive, adjective, participle1, participle2, verb1, verb2, noun, superlative, derivational,
	} {
		slices.SortFunc(l, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	}
}

// minStemRunes is the shortest stem kept: a two-letter one would find too
// much.
const minStemRunes = 3

// stemRussian is the stem of a lower-case Russian word by the Snowball
// algorithm, "ё" read as "е". A word of up to three letters, and one whose
// stem would be shorter than that, stays whole.
func stemRussian(word string) string {
	word = strings.ReplaceAll(word, "ё", "е")
	w := []rune(word)
	if len(w) <= minStemRunes {
		return word
	}
	rv, r2 := regions(w)
	s := stemmer{w: w, rv: rv}
	if !s.cut(gerund1, true) && !s.cut(gerund2, false) {
		s.cut(reflexive, false)
		if !s.adjectival() && !s.cut(verb1, true) && !s.cut(verb2, false) {
			s.cut(noun, false)
		}
	}
	s.cut(finalI, false)
	if n := s.match(derivational, false, r2); n != 0 {
		s.w = s.w[:len(s.w)-n]
	}
	if s.undouble() {
		// A doubled final "н" is the whole of the last step.
	} else if s.cut(superlative, false) {
		s.undouble()
	} else {
		s.cut(softSign, false)
	}
	if len(s.w) < minStemRunes {
		return word
	}
	return string(s.w)
}

// regions finds where RV and R2 of w begin: RV after the first vowel, R1
// after the first consonant following a vowel, R2 the same within R1.
func regions(w []rune) (rv, r2 int) {
	rv = len(w)
	for i, r := range w {
		if isVowel(r) {
			rv = i + 1
			break
		}
	}
	after := func(start int) int {
		for i := start + 1; i < len(w); i++ {
			if !isVowel(w[i]) && isVowel(w[i-1]) {
				return i + 1
			}
		}
		return len(w)
	}
	return rv, after(after(0))
}

func isVowel(r rune) bool {
	return strings.ContainsRune("аеиоуыэюя", r)
}

type stemmer struct {
	w  []rune
	rv int
}

// match finds the longest of endings at the end of s.w within the region
// from, preceded by "а" or "я" of the region when group1, and gives its
// length.
func (s *stemmer) match(endings []string, group1 bool, from int) int {
	for _, e := range endings {
		n := len([]rune(e))
		i := len(s.w) - n
		if i < from || string(s.w[i:]) != e {
			continue
		}
		if group1 && (i-1 < s.rv || (s.w[i-1] != 'а' && s.w[i-1] != 'я')) {
			continue
		}
		return n
	}
	return 0
}

func (s *stemmer) cut(endings []string, group1 bool) bool {
	n := s.match(endings, group1, s.rv)
	if n == 0 {
		return false
	}
	s.w = s.w[:len(s.w)-n]
	return true
}

// adjectival cuts an adjective's ending and the participle's before it.
func (s *stemmer) adjectival() bool {
	if !s.cut(adjective, false) {
		return false
	}
	if !s.cut(participle1, true) {
		s.cut(participle2, false)
	}
	return true
}

// undouble turns a final "нн" of RV into "н".
func (s *stemmer) undouble() bool {
	if n := len(s.w); n >= 2 && n-2 >= s.rv && s.w[n-1] == 'н' && s.w[n-2] == 'н' {
		s.w = s.w[:n-1]
		return true
	}
	return false
}
