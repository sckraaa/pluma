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

// Package msgsearch is how messages are found: the text of a query, the
// kinds of messages of messages.search, and the queries over the boxes of
// users and over the messages of channels (task 066, ADR 0039).
package msgsearch

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	// maxWords and maxWordRunes bound what one query costs.
	maxWords     = 8
	maxWordRunes = 64
)

// wordStart is a word's left edge: what comes before it is no letter or
// digit, so "кот" finds "кот" and "Коты" but not "скот".
const wordStart = `(?<![\p{L}\p{N}])`

// tagEnd is a tag's right edge: a hashtag or a cashtag is found whole, not
// as the beginning of a longer one.
const tagEnd = `(?![\p{L}\p{N}_])`

// sentencePunct is what is cut from the ends of a word; the other signs
// are part of what is looked for ("50%", "c++", "кот-пёс").
const sentencePunct = `.,!?:;"'«»()[]{}…—–`

// Patterns are the regular expressions (ICU, as REGEXP_LIKE takes them) a
// message must all match for query q; none for an empty query. Every word
// of q is a condition of its own, in any order and place of the text, each
// matching from the start of a word; a word is a prefix so that a query
// still being typed finds. A Russian word is cut to its stem first, so
// that a query finds the other forms of the word. "#tag" and "$TAG" are
// found whole, the case aside. The letters "е" and "ё" are one. A query of
// punctuation alone is looked for as it is.
func Patterns(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var out []string
	for _, word := range strings.Fields(q) {
		if len(out) == maxWords {
			break
		}
		if p := wordPattern(word); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		// Nothing but punctuation: the text itself, anywhere.
		return []string{quote(q)}
	}
	return out
}

func wordPattern(word string) string {
	var sigil string
	if word[0] == '#' || word[0] == '$' {
		sigil, word = word[:1], word[1:]
	}
	word = strings.Trim(word, sentencePunct)
	if word == "" {
		return ""
	}
	if r := []rune(word); len(r) > maxWordRunes {
		word = string(r[:maxWordRunes])
	}
	if sigil != "" {
		return wordStart + quote(sigil+word) + tagEnd
	}
	if cyrillicWord(word) {
		word = stemRussian(word)
	}
	if first := []rune(word)[0]; unicode.IsLetter(first) || unicode.IsNumber(first) {
		return wordStart + quote(word)
	}
	return quote(word)
}

// quote escapes s for a pattern, with "е" and "ё" as one letter.
func quote(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 'е' || r == 'ё' {
			b.WriteString("[её]")
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	return b.String()
}

func cyrillicWord(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Cyrillic, r) || !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}
