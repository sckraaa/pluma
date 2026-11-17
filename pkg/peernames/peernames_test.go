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

package peernames

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

var testRows = []Row{
	{Id: 1, Username: "Main", Editable: true, Active: true},
	{Id: 2, Username: "hidden"},
	{Id: 3, Username: "extra", Active: true},
}

// An order of usernames lists each shown one once, without case, and no
// hidden one; it places each shown one at its index; the order they have
// is ErrSameOrder.
func TestOrder(t *testing.T) {
	for _, c := range []struct {
		order []string
		err   error
		at    map[int64]int
	}{
		{[]string{"extra", "main"}, nil, map[int64]int{3: 0, 1: 1}},
		{[]string{"EXTRA", "Main"}, nil, map[int64]int{3: 0, 1: 1}},
		{[]string{"main", "extra"}, ErrSameOrder, nil},
		{[]string{"MAIN", "Extra"}, ErrSameOrder, nil},
		{[]string{"main"}, ErrBadOrder, nil},
		{[]string{"main", "extra", "hidden"}, ErrBadOrder, nil},
		{[]string{"main", "hidden"}, ErrBadOrder, nil},
		{[]string{"main", "main"}, ErrBadOrder, nil},
		{[]string{"main", "extra", "extra"}, ErrBadOrder, nil},
		{[]string{"main", "nobody"}, ErrBadOrder, nil},
		{nil, ErrBadOrder, nil},
	} {
		at, err := Order(testRows, c.order)
		if !errors.Is(err, c.err) {
			t.Errorf("%v: %v, want %v", c.order, err, c.err)
			continue
		}
		for id, i := range c.at {
			if at[id] != i {
				t.Errorf("%v: row %d at %d, want %d", c.order, id, at[id], i)
			}
		}
	}
	if names := Active(testRows); !slices.Equal(names, []string{"Main", "extra"}) {
		t.Errorf("shown: %v", names)
	}
}

// A peer with its editable username alone, the case of every account
// today: its one name in its order is ErrSameOrder, an empty order with no
// username too, and any other order ErrBadOrder.
func TestOrderOfOne(t *testing.T) {
	one := []Row{{Id: 1, Username: "alice", Editable: true, Active: true}}
	if _, err := Order(one, []string{"Alice"}); !errors.Is(err, ErrSameOrder) {
		t.Errorf("its one name: %v, want ErrSameOrder", err)
	}
	if _, err := Order(one, []string{"bob"}); !errors.Is(err, ErrBadOrder) {
		t.Errorf("another's name: %v, want ErrBadOrder", err)
	}
	if _, err := Order(nil, nil); !errors.Is(err, ErrSameOrder) {
		t.Errorf("no names, no order: %v, want ErrSameOrder", err)
	}
	if _, err := Order(nil, []string{"alice"}); !errors.Is(err, ErrBadOrder) {
		t.Errorf("no names, an order: %v, want ErrBadOrder", err)
	}
}

// Toggle: the state a username has comes first, the editable one is never
// hidden, at most MaxShown are shown, and another's name is not found.
func TestToggled(t *testing.T) {
	for _, c := range []struct {
		username string
		active   bool
		err      error
		id       int64
	}{
		{"main", true, ErrSame, 0},
		{"MAIN", false, ErrEditable, 0},
		{"hidden", false, ErrSame, 0},
		{"Hidden", true, nil, 2},
		{"extra", false, nil, 3},
		{"extra", true, ErrSame, 0},
		{"nobody", true, ErrNotFound, 0},
		{"", false, ErrNotFound, 0},
	} {
		r, err := toggled(testRows, c.username, c.active)
		if !errors.Is(err, c.err) || r.Id != c.id {
			t.Errorf("%s %v: row %d, %v; want row %d, %v", c.username, c.active, r.Id, err, c.id, c.err)
		}
	}

	full := []Row{{Id: 100, Username: "main", Editable: true, Active: true}}
	for i := 1; i < MaxShown; i++ {
		full = append(full, Row{Id: int64(i), Username: fmt.Sprintf("shown%d", i), Active: true})
	}
	full = append(full, Row{Id: 200, Username: "spare"})
	if _, err := toggled(full, "spare", true); !errors.Is(err, ErrTooMany) {
		t.Errorf("an eleventh shown: %v, want ErrTooMany", err)
	}
	if r, err := toggled(full, "shown3", false); err != nil || r.Id != 3 {
		t.Errorf("hiding one of ten: row %d, %v", r.Id, err)
	}
	if r, err := toggled(full[1:], "spare", true); err != nil || r.Id != 200 {
		t.Errorf("a tenth shown: row %d, %v", r.Id, err)
	}
}
