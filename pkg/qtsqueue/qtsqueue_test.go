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

package qtsqueue

import (
	"slices"
	"strings"
	"testing"
)

// The users of an update go in the column as decimal ids joined by
// commas, as many as fit, and come back the same.
func TestUserIds(t *testing.T) {
	if s := joinIds(nil); s != "" {
		t.Fatalf("no ids: %q", s)
	}
	ids := []int64{1, 777000, 1 << 40}
	back, err := splitIds(joinIds(ids))
	if err != nil || !slices.Equal(back, ids) {
		t.Fatalf("%v back as %v, %v", ids, back, err)
	}
	many := make([]int64, 200)
	for i := range many {
		many[i] = 1_000_000_000_000 + int64(i)
	}
	s := joinIds(many)
	if len(s) > userIdsMax || strings.HasSuffix(s, ",") {
		t.Fatalf("%d bytes: %q", len(s), s)
	}
	back, err = splitIds(s)
	if err != nil || len(back) == 0 || !slices.Equal(back, many[:len(back)]) {
		t.Fatalf("the ids that fit: %v, %v", back, err)
	}
	if _, err = splitIds("1,x"); err == nil {
		t.Fatal("a column that is no list of ids")
	}
}
