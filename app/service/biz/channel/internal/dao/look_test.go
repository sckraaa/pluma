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

import "testing"

// An order of usernames lists each shown one once, without case, and no
// hidden one; it places each shown one at its index.
func TestUsernamesOrder(t *testing.T) {
	rows := []usernameRow{
		{Id: 1, Username: "Main", Editable: true, Active: true},
		{Id: 2, Username: "hidden"},
		{Id: 3, Username: "extra", Active: true},
	}
	for _, c := range []struct {
		order []string
		ok    bool
		at    map[int64]int
	}{
		{[]string{"extra", "main"}, true, map[int64]int{3: 0, 1: 1}},
		{[]string{"MAIN", "Extra"}, true, map[int64]int{1: 0, 3: 1}},
		{[]string{"main"}, false, nil},
		{[]string{"main", "extra", "hidden"}, false, nil},
		{[]string{"main", "main"}, false, nil},
		{[]string{"main", "extra", "extra"}, false, nil},
		{nil, false, nil},
	} {
		at, ok := usernamesOrder(rows, c.order)
		if ok != c.ok {
			t.Errorf("%v: ok %v, want %v", c.order, ok, c.ok)
			continue
		}
		for id, i := range c.at {
			if at[id] != i {
				t.Errorf("%v: row %d at %d, want %d", c.order, id, at[id], i)
			}
		}
	}
	if names := activeNames(rows); len(names) != 2 || names[0] != "Main" || names[1] != "extra" {
		t.Errorf("shown: %v", names)
	}
}
