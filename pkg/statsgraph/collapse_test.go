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

package statsgraph

import (
	"reflect"
	"testing"
)

func TestCollapse(t *testing.T) {
	s := []Series{
		{NameKey: "a", Values: []int64{1, 1}},
		{NameKey: "b", Values: []int64{5, 5}},
		{NameKey: "c", Values: []int64{2, 0}},
		{NameKey: "d", Values: []int64{0, 2}},
		{NameKey: "e", Values: []int64{3, 3}},
	}
	got := Collapse(s, 2, Series{NameKey: "other", Color: Golden, Values: []int64{100, 100}})
	want := []Series{
		{NameKey: "b", Values: []int64{5, 5}},
		{NameKey: "e", Values: []int64{3, 3}},
		{NameKey: "other", Color: Golden, Values: []int64{3, 3}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}

	// Equal totals (a, c and d have 2) keep their order: a and c stay, d
	// goes to other.
	got = Collapse(s, 4, Series{NameKey: "other"})
	if len(got) != 5 || got[2].NameKey != "a" || got[3].NameKey != "c" || got[4].NameKey != "other" || !reflect.DeepEqual(got[4].Values, []int64{0, 2}) {
		t.Errorf("ties: %+v", got)
	}

	if got := Collapse(s, 5, Series{NameKey: "other"}); !reflect.DeepEqual(got, s) {
		t.Errorf("all fit: %+v", got)
	}
	if got := Collapse(s, 0, Series{NameKey: "other"}); len(got) != 1 || !reflect.DeepEqual(got[0].Values, []int64{11, 11}) {
		t.Errorf("none kept: %+v", got)
	}
	if s[0].NameKey != "a" || s[1].NameKey != "b" {
		t.Error("the input was reordered")
	}
}
