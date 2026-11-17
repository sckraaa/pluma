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

package channelstats

import (
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type execCall struct {
	query string
	args  []any
}

// fakeTx answers the ghost query with ghosts and records the statements.
type fakeTx struct {
	ghosts []int64
	since  []int64
	execs  []execCall
}

func (f *fakeTx) Exec(query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, execCall{query, args})
	return nil, nil
}

func (f *fakeTx) QueryRowsPartial(v any, query string, args ...any) error {
	out, ok := v.(*[]int64)
	if !ok {
		return errors.New("unexpected destination")
	}
	switch {
	case strings.Contains(query, "FROM users WHERE ghost = 1"):
		for _, a := range args {
			if id := a.(int64); slices.Contains(f.ghosts, id) {
				*out = append(*out, id)
			}
		}
	case strings.Contains(query, "FROM stats_collection"):
		*out = append(*out, f.since...)
	default:
		return errors.New("unexpected query " + query)
	}
	return nil
}

func TestDay(t *testing.T) {
	for in, want := range map[int64]int32{
		0:          0,
		86399:      0,
		86400:      1,
		1763251200: 20408, // 2025-11-16 00:00 UTC
		1763337599: 20408,
		-1:         -1,
		-86400:     -1,
		-86401:     -2,
	} {
		if got := Day(in); got != want {
			t.Errorf("Day(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestMembersJoinedLeavesGhostsOut(t *testing.T) {
	tx := &fakeTx{ghosts: []int64{3}}
	if err := MembersJoined(tx, 10, []int64{2, 3, 4}, 1000, JoinAdded, 1); err != nil {
		t.Fatal(err)
	}
	if len(tx.execs) != 1 {
		t.Fatalf("statements: %d, want 1", len(tx.execs))
	}
	want := []any{int64(10), int64(2), int64(1000), true, int8(JoinAdded), int64(1), int64(10), int64(4), int64(1000), true, int8(JoinAdded), int64(1)}
	if !reflect.DeepEqual(tx.execs[0].args, want) {
		t.Fatalf("args %v, want %v", tx.execs[0].args, want)
	}
	if !strings.Contains(tx.execs[0].query, "VALUES (?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?)") {
		t.Fatalf("query %q", tx.execs[0].query)
	}
}

func TestGhostWritesNothing(t *testing.T) {
	tx := &fakeTx{ghosts: []int64{3}}
	for _, err := range []error{
		MemberJoined(tx, 10, 3, 1000, JoinLink, 1),
		MemberLeft(tx, 10, 3, 1000),
		MuteChanged(tx, 10, 3, 1000, MuteForever),
		Reader(tx, 10, 3, 1000),
		AdminAction(tx, 10, 3, 1000, Deleted, 2),
		MembersJoined(tx, 10, []int64{3, 0, -5}, 1000, JoinAdded, 1),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(tx.execs) != 0 {
		t.Fatalf("a ghost was written: %v", tx.execs)
	}
}

func TestMemberLeft(t *testing.T) {
	tx := &fakeTx{}
	if err := MemberLeft(tx, 10, 2, 1000); err != nil {
		t.Fatal(err)
	}
	want := []any{int64(10), int64(2), int64(1000), false, int8(JoinOther), int64(0)}
	if len(tx.execs) != 1 || !reflect.DeepEqual(tx.execs[0].args, want) {
		t.Fatalf("statements %v, want one with %v", tx.execs, want)
	}
}

func TestInvalidArguments(t *testing.T) {
	tx := &fakeTx{}
	for name, err := range map[string]error{
		"source":      MemberJoined(tx, 10, 2, 1000, JoinSource(6), 0),
		"negative":    MemberJoined(tx, 10, 2, 1000, JoinSource(-1), 0),
		"mute":        MuteChanged(tx, 10, 2, 1000, -1),
		"action":      AdminAction(tx, 10, 2, 1000, Action(9), 1),
		"forwardKind": PublicForward(tx, ForwardKind(3), 4, 10, 1, 20, 2, 1000),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if len(tx.execs) != 0 {
		t.Fatalf("written: %v", tx.execs)
	}
}

func TestAdminAction(t *testing.T) {
	tx := &fakeTx{}
	if err := AdminAction(tx, 10, 2, 86400*3+5, Kicked, 1); err != nil {
		t.Fatal(err)
	}
	if err := AdminAction(tx, 10, 2, 86400*3+5, Deleted, 0); err != nil {
		t.Fatal(err)
	}
	if len(tx.execs) != 1 {
		t.Fatalf("statements: %d, want 1 (n 0 writes nothing)", len(tx.execs))
	}
	q := tx.execs[0].query
	if !strings.Contains(q, "(channel_id, `day`, admin_id, kicked)") || !strings.Contains(q, "kicked = kicked + VALUES(kicked)") {
		t.Fatalf("query %q", q)
	}
	if want := []any{int64(10), int32(3), int64(2), 1}; !reflect.DeepEqual(tx.execs[0].args, want) {
		t.Fatalf("args %v, want %v", tx.execs[0].args, want)
	}
}

func TestReaderDay(t *testing.T) {
	tx := &fakeTx{}
	if err := Reader(tx, 10, 2, 86400*7+86399); err != nil {
		t.Fatal(err)
	}
	if want := []any{int64(10), int32(7), int64(2)}; len(tx.execs) != 1 || !reflect.DeepEqual(tx.execs[0].args, want) {
		t.Fatalf("statements %v, want one with %v", tx.execs, want)
	}
	if !strings.HasPrefix(tx.execs[0].query, "INSERT IGNORE") {
		t.Fatalf("a reader counts once a day: %q", tx.execs[0].query)
	}
}

func TestSince(t *testing.T) {
	if _, err := Since(&fakeTx{}, Readers); !errors.Is(err, ErrNotCollected) {
		t.Fatalf("no row: %v, want ErrNotCollected", err)
	}
	got, err := Since(&fakeTx{since: []int64{1234}}, Readers)
	if err != nil || got != 1234 {
		t.Fatalf("Since = %d, %v", got, err)
	}
}
