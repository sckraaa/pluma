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

package dao

import (
	"slices"
	"testing"
)

func TestReorderPinned(t *testing.T) {
	cases := []struct {
		name       string
		cur, order []int32
		force      bool
		want       []int32
	}{
		{"force replaces", []int32{5, 3}, []int32{3, 7}, true, []int32{3, 7}},
		{"force keeps the first of repeats", nil, []int32{4, 4, 9}, true, []int32{4, 9}},
		{"listed swap places", []int32{5, 3, 9}, []int32{9, 5}, false, []int32{9, 3, 5}},
		{"not pinned is ignored", []int32{5, 3}, []int32{7}, false, []int32{5, 3}},
		{"omitted stays pinned", []int32{5, 3, 9}, []int32{3}, false, []int32{5, 3, 9}},
	}
	for _, c := range cases {
		if got := reorderPinned(c.cur, c.order, c.force); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTopicTitle(t *testing.T) {
	if _, err := topicTitle("   "); err == nil {
		t.Error("a blank title is taken")
	}
	long := make([]rune, TopicTitleMax+5)
	for i := range long {
		long[i] = 'ы'
	}
	got, err := topicTitle(string(long))
	if err != nil || len([]rune(got)) != TopicTitleMax {
		t.Errorf("a long title: %d runes, %v", len([]rune(got)), err)
	}
}
