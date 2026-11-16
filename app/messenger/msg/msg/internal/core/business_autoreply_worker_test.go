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

package core

import (
	"slices"
	"testing"

	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
)

// The random ids of a queued automatic reply are the same for every
// attempt, apart for every message and every row, and valid random ids:
// positive, never 0.
func TestAutoReplyRandomIds(t *testing.T) {
	a := autoReplyRandomIds(41, 3)
	if !slices.Equal(a, autoReplyRandomIds(41, 3)) {
		t.Fatal("two attempts of a row get other random ids")
	}
	if b := autoReplyRandomIds(41, 5); !slices.Equal(a, b[:3]) {
		t.Errorf("the first ids depend on the count: %v and %v", a, b)
	}
	seen := map[int64]bool{}
	for id := int64(1); id <= 200; id++ {
		for _, r := range autoReplyRandomIds(id, 4) {
			if r <= 0 {
				t.Fatalf("row %d: random id %d", id, r)
			}
			if seen[r] {
				t.Fatalf("row %d: random id %d repeats", id, r)
			}
			seen[r] = true
		}
	}
	if dao.RepeatedRandomIds(autoReplyRandomIds(7, 20)) {
		t.Error("a reply of 20 messages repeats a random id")
	}
}
