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

package business

import (
	"crypto/rand"
	"fmt"
)

// SlugLength is the length of the slug of a chat link: the path of
// https://pluma.chat/m/<slug> (ADR 0067, item 5).
const SlugLength = 16

const slugAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// NewSlug returns a new slug of a chat link: SlugLength characters of
// [A-Za-z0-9] from the cryptographic generator, each of the 62 equally
// likely (bytes from 248 up are drawn again: 248 is 4×62). The slug is the
// only secret of the link, so it is not guessable; the caller draws again
// on the rare collision with a kept one.
func NewSlug() (string, error) {
	out := make([]byte, 0, SlugLength)
	buf := make([]byte, SlugLength*2)
	for len(out) < SlugLength {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("business: slug: %w", err)
		}
		for _, b := range buf {
			if b >= 248 {
				continue
			}
			out = append(out, slugAlphabet[int(b)%len(slugAlphabet)])
			if len(out) == SlugLength {
				break
			}
		}
	}
	return string(out), nil
}

// ValidSlug tells whether s could be a slug NewSlug made: a slug sent by a
// client is checked before it is looked up.
func ValidSlug(s string) bool {
	if len(s) != SlugLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if alnum := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'; !alnum {
			return false
		}
	}
	return true
}
