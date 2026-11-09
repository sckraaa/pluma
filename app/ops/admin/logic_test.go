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

package main

import (
	"slices"
	"strings"
	"testing"
)

func TestMaskPhone(t *testing.T) {
	for in, want := range map[string]string{"79991234567": "+…4567", "1234": "+…", "": "+…"} {
		if got := maskPhone(in); got != want {
			t.Errorf("maskPhone(%q) = %q, want %q", in, got, want)
		}
	}
	if strings.Contains(maskPhone("79991234567"), "7999") {
		t.Error("the mask shows the start of the number")
	}
}

func TestUserSearchWhere(t *testing.T) {
	for _, tc := range []struct {
		q     string
		has   string
		nargs int
	}{
		{"", "", 0},
		{"   ", "", 0},
		{"@alice", "u.username LIKE", 1},
		{"+7999", "u.phone LIKE", 2},
		{"1000042", "u.id = ?", 2},
		{"Анна", "u.first_name LIKE", 3},
	} {
		where, args := userSearchWhere(tc.q)
		if tc.has != "" && !strings.Contains(where, tc.has) {
			t.Errorf("%q: where %q has no %q", tc.q, where, tc.has)
		}
		if len(args) != tc.nargs {
			t.Errorf("%q: %d args, want %d", tc.q, len(args), tc.nargs)
		}
	}
}

func TestLikeEscape(t *testing.T) {
	if got := likeEscape(`10%_\`); got != `10\%\_\\` {
		t.Errorf("got %q", got)
	}
}

func TestParseAudience(t *testing.T) {
	ids, err := parseAudience(&broadcastBody{Audience: audienceIds, Ids: "3, 1 2;2\n5"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int64{1, 2, 3, 5}) {
		t.Errorf("ids: %v", ids)
	}
	if _, err := parseAudience(&broadcastBody{Audience: audienceAll}); err != nil {
		t.Error(err)
	}
	for _, b := range []broadcastBody{
		{Audience: "nobody"},
		{Audience: audienceChannel},
		{Audience: audienceIds, Ids: ""},
		{Audience: audienceIds, Ids: "abc"},
	} {
		if _, err := parseAudience(&b); err == nil {
			t.Errorf("%+v: no error", b)
		}
	}
}
