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

package urlauth

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// MatchCodeCount is the number of match codes an app offers
// (urlAuthResultRequest.match_codes, ADR 0068 p. 6); one of them, the right
// one, is shown on the page of the request.
const MatchCodeCount = 4

// matchCodeSet are the emoji the match codes are drawn from. Each is one
// code point of Unicode 9.0 or older with an emoji presentation of its own
// (no variation selector): tdesktop draws a code only when the string is
// exactly one emoji (boxes/url_auth_box.cpp:99-104), and every one of them
// is in the emoji tables of Android (messenger/EmojiData.java) and tweb
// (src/config/emoji.ts); iOS draws system emoji. The animals and fruit are
// told apart at a glance on a small screen.
var matchCodeSet = [...]string{
	"🐶", "🐱", "🐭", "🐹", "🐰", "🦊", "🐻", "🐼",
	"🐨", "🐯", "🦁", "🐮", "🐷", "🐸", "🐵", "🐔",
	"🐧", "🐤", "🦉", "🐴", "🦄", "🐝", "🐢", "🐙",
	"🐬", "🐳", "🍎", "🍋", "🍉", "🍇", "🍓", "🍒",
}

// NewMatchCodes draws MatchCodeCount different emoji of the set in a random
// order and picks the right one among them, all with crypto/rand.
func NewMatchCodes() (codes []string, right string, err error) {
	pool := matchCodeSet
	// A partial Fisher-Yates shuffle: the first MatchCodeCount places.
	for i := 0; i < MatchCodeCount; i++ {
		j, err := randInt(len(pool) - i)
		if err != nil {
			return nil, "", err
		}
		pool[i], pool[i+j] = pool[i+j], pool[i]
	}
	codes = append([]string(nil), pool[:MatchCodeCount]...)
	k, err := randInt(MatchCodeCount)
	if err != nil {
		return nil, "", err
	}
	return codes, codes[k], nil
}

func randInt(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, fmt.Errorf("urlauth: read random: %w", err)
	}
	return int(v.Int64()), nil
}
