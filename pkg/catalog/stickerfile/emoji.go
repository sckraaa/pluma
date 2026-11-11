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

package stickerfile

import (
	"strings"
	"unicode/utf8"
)

// MaxEmoji is the most emoji one sticker stands for: this server's bound;
// the clients send one (Android StickerMakerView, Telegram Desktop) or the
// few the user picked (iOS ImportStickers.swift:256, emojis.joined()).
const MaxEmoji = 20

// SplitEmoji splits the emoji of a sticker (inputStickerSetItem.emoji: the
// emoji it stands for, one after another) into the emoji themselves, each
// once, in their order. It is false for a string that is empty, has
// anything but emoji, or has more than MaxEmoji of them.
func SplitEmoji(s string) ([]string, bool) {
	var out []string
	for s = strings.TrimSpace(s); s != ""; {
		n := emojiLen(s)
		if n == 0 {
			return nil, false
		}
		e := s[:n]
		s = s[n:]
		dup := false
		for _, have := range out {
			dup = dup || have == e
		}
		if !dup {
			out = append(out, e)
		}
		if len(out) > MaxEmoji {
			return nil, false
		}
	}
	return out, len(out) > 0
}

// EmojiPrefix is the length in bytes of the emoji s starts with, 0 if it
// starts with none.
func EmojiPrefix(s string) int { return emojiLen(s) }

// emojiLen is the length in bytes of the emoji s starts with, 0 if it
// starts with none: a base, its variation selector, skin tone or keycap,
// tag characters, and what a zero-width joiner joins to it; or a flag of
// two regional indicators.
func emojiLen(s string) int {
	r, n := utf8.DecodeRuneInString(s)
	switch {
	case isRegional(r):
		if r2, n2 := utf8.DecodeRuneInString(s[n:]); isRegional(r2) {
			return n + n2
		}
		return 0
	case r == '#' || r == '*' || (r >= '0' && r <= '9'):
		// A keycap: the character, an optional selector, the keycap mark.
		rest := strings.TrimPrefix(s[n:], "️")
		if !strings.HasPrefix(rest, "⃣") {
			return 0
		}
		return len(s) - len(rest) + len("⃣")
	case !isEmojiBase(r):
		return 0
	}
	for {
		r, m := utf8.DecodeRuneInString(s[n:])
		switch {
		case m == 0:
			return n
		case r == 0xfe0f || r == 0xfe0e || r == 0x20e3 || (r >= 0x1f3fb && r <= 0x1f3ff) || (r >= 0xe0020 && r <= 0xe007f):
			n += m
		case r == 0x200d:
			next, k := utf8.DecodeRuneInString(s[n+m:])
			if !isEmojiBase(next) {
				return 0
			}
			n += m + k
		default:
			return n
		}
	}
}

func isRegional(r rune) bool { return r >= 0x1f1e6 && r <= 0x1f1ff }

// isEmojiBase tells if r starts an emoji: a rune of the blocks emoji come
// from. It is wider than the list of emoji of any Unicode version, so that
// a new emoji is taken; it leaves out letters, digits and punctuation.
func isEmojiBase(r rune) bool {
	switch {
	case r == 0xa9 || r == 0xae, // © ®
		r == 0x203c || r == 0x2049 || r == 0x2122 || r == 0x2139,
		r >= 0x2190 && r <= 0x21ff, // arrows
		r >= 0x2300 && r <= 0x23ff, // technical
		r >= 0x2460 && r <= 0x24ff, // enclosed alphanumerics
		r >= 0x25a0 && r <= 0x27bf, // shapes, symbols, dingbats
		r >= 0x2900 && r <= 0x297f,
		r >= 0x2b00 && r <= 0x2bff,
		r == 0x3030 || r == 0x303d || r == 0x3297 || r == 0x3299,
		r >= 0x1f000 && r <= 0x1faff && !isRegional(r) && (r < 0x1f3fb || r > 0x1f3ff):
		return true
	}
	return false
}

// SplitKeywords splits the keywords of a sticker
// (inputStickerSetItem.keywords: "separated by commas",
// core.telegram.org/constructor/inputStickerSetItem) into the words, each
// once, without the empty ones.
func SplitKeywords(s string) []string {
	out := []string{}
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		dup := k == ""
		for _, have := range out {
			dup = dup || strings.EqualFold(have, k)
		}
		if !dup {
			out = append(out, k)
		}
	}
	return out
}
