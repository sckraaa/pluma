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

package stories

import (
	"slices"
	"strings"
	"testing"
)

// The named ones first in the order given, the rest after them in their
// order; ids that are not there and repeats are passed over.
func TestNamedFirst(t *testing.T) {
	for _, c := range []struct {
		current, order, want []int32
	}{
		{[]int32{1, 2, 3, 4}, []int32{3, 1}, []int32{3, 1, 2, 4}},
		{[]int32{1, 2, 3}, []int32{9, 2, 2, 8}, []int32{2, 1, 3}},
		{[]int32{1, 2, 3}, nil, []int32{1, 2, 3}},
		{[]int32{1, 2, 3}, []int32{3, 2, 1}, []int32{3, 2, 1}},
		{nil, []int32{1}, []int32{}},
	} {
		if got := namedFirst(c.current, c.order); !slices.Equal(got, c.want) {
			t.Errorf("namedFirst(%v, %v) = %v, want %v", c.current, c.order, got, c.want)
		}
	}
}

func TestUniqueIds(t *testing.T) {
	if got := uniqueIds([]int32{3, 1, 3, 0, -2, 1, 5}); !slices.Equal(got, []int32{3, 1, 5}) {
		t.Errorf("uniqueIds = %v", got)
	}
}

// A title is trimmed and counted in UTF-16 units, as the clients cut it.
func TestAlbumTitle(t *testing.T) {
	for _, c := range []struct {
		raw, want string
		ok        bool
	}{
		{"Trips", "Trips", true},
		{"  Trips \n", "Trips", true},
		{strings.Repeat("a", MaxAlbumTitle), strings.Repeat("a", MaxAlbumTitle), true},
		{strings.Repeat("a", MaxAlbumTitle+1), strings.Repeat("a", MaxAlbumTitle+1), false},
		{strings.Repeat("я", MaxAlbumTitle), strings.Repeat("я", MaxAlbumTitle), true},
		// An emoji outside the BMP is two units.
		{strings.Repeat("😀", MaxAlbumTitle/2), strings.Repeat("😀", MaxAlbumTitle/2), true},
		{strings.Repeat("😀", MaxAlbumTitle/2) + "a", strings.Repeat("😀", MaxAlbumTitle/2) + "a", false},
		{"   ", "", false},
		{"", "", false},
	} {
		got, ok := AlbumTitle(c.raw)
		if got != c.want || ok != c.ok {
			t.Errorf("AlbumTitle(%q) = %q, %v, want %q, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// storyItem.albums: what SeenOf read wins over the story's own, which a
// Seen made by hand falls back to; none leaves the field unset.
func TestItemAlbums(t *testing.T) {
	st := &Story{Peer: UserPeer(1), Id: 4, Date: 100, ExpireDate: 200, Albums: []int32{7, 3}}
	if got := Item(st, Seen{}).GetAlbums(); !slices.Equal(got, []int32{7, 3}) {
		t.Errorf("albums of the story: %v", got)
	}
	if got := Item(st, Seen{Albums: []int32{5}}).GetAlbums(); !slices.Equal(got, []int32{5}) {
		t.Errorf("albums SeenOf read: %v", got)
	}
	if got := Item(st, Seen{Albums: []int32{}}).GetAlbums(); got != nil {
		t.Errorf("no albums SeenOf read: %v, want unset", got)
	}
	st.Albums = nil
	if got := Item(st, Seen{}).GetAlbums(); got != nil {
		t.Errorf("no albums: %v, want unset", got)
	}
	st.Albums = []int32{}
	if got := Item(st, Seen{}).GetAlbums(); got != nil {
		t.Errorf("empty albums: %v, want unset", got)
	}
}
