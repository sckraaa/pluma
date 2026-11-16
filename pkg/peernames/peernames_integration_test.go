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

//go:build integration

package peernames

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

func startDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	boxDB, err := userbox.Open(db.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boxDB.Close() })
	return boxDB
}

// seed gives the peer the usernames names in their order; the first is
// the editable one, and those in shown are shown.
func seed(t *testing.T, db *sql.DB, peerType int32, peerId int64, names []string, shown ...string) {
	t.Helper()
	for i, n := range names {
		if _, err := db.Exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, ?, ?, ?)",
			n, peerType, peerId, i == 0, i == 0 || slices.Contains(shown, n), i); err != nil {
			t.Fatal(err)
		}
	}
}

// state is the usernames of the peer in their order, each with + when
// shown.
func state(t *testing.T, db *sql.DB, peerType int32, peerId int64) string {
	t.Helper()
	rows, err := db.Query("SELECT username, active FROM username WHERE peer_type = ? AND peer_id = ? ORDER BY order2, id", peerType, peerId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			n      string
			active bool
		)
		if err = rows.Scan(&n, &active); err != nil {
			t.Fatal(err)
		}
		if active {
			n += "+"
		}
		out = append(out, n)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, " ")
}

// edit runs fn on the peer's rows in a box transaction of the account by.
func edit(t *testing.T, db *sql.DB, by int64, peerType int32, peerId int64, fn func(b *userbox.Box, rows []Row) error) error {
	t.Helper()
	_, err := userbox.Run(context.Background(), db, by, func(b *userbox.Box) error {
		rows, err := Load(b, peerType, peerId)
		if err != nil {
			return err
		}
		return fn(b, rows)
	})
	return err
}

// Toggle and Reorder write what they decide, one peer apart from another
// with the same id of another type, and an error writes nothing.
func TestToggleReorder(t *testing.T) {
	db := startDB(t)
	const id = int64(4100)
	seed(t, db, mtproto.PEER_USER, id, []string{"u_main", "u_one", "u_two", "u_three"}, "u_two")
	seed(t, db, mtproto.PEER_CHANNEL, id, []string{"c_main", "c_one"}, "c_one")

	toggle := func(name string, active bool) error {
		return edit(t, db, id, mtproto.PEER_USER, id, func(b *userbox.Box, rows []Row) error {
			return Toggle(b, rows, name, active)
		})
	}
	reorder := func(order ...string) error {
		return edit(t, db, id, mtproto.PEER_USER, id, func(b *userbox.Box, rows []Row) error {
			return Reorder(b, rows, order)
		})
	}
	if err := toggle("U_ONE", true); err != nil {
		t.Fatal(err)
	}
	if got, want := state(t, db, mtproto.PEER_USER, id), "u_main+ u_one+ u_two+ u_three"; got != want {
		t.Fatalf("after showing u_one: %q, want %q", got, want)
	}
	if err := toggle("c_one", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("the channel's name: %v, want ErrNotFound", err)
	}
	if err := toggle("u_main", false); !errors.Is(err, ErrEditable) {
		t.Errorf("hiding the editable one: %v, want ErrEditable", err)
	}
	// The hidden ones follow the shown ones in their order.
	if err := reorder("u_two", "U_MAIN", "u_one"); err != nil {
		t.Fatal(err)
	}
	if got, want := state(t, db, mtproto.PEER_USER, id), "u_two+ u_main+ u_one+ u_three"; got != want {
		t.Fatalf("after the reorder: %q, want %q", got, want)
	}
	if err := reorder("u_two", "u_main", "u_one"); !errors.Is(err, ErrSameOrder) {
		t.Errorf("the same order: %v, want ErrSameOrder", err)
	}
	if err := reorder("u_two", "u_main"); !errors.Is(err, ErrBadOrder) {
		t.Errorf("an order without u_one: %v, want ErrBadOrder", err)
	}
	if err := toggle("u_two", false); err != nil {
		t.Fatal(err)
	}
	if err := reorder("u_one", "u_main"); err != nil {
		t.Fatal(err)
	}
	if got, want := state(t, db, mtproto.PEER_USER, id), "u_one+ u_main+ u_two u_three"; got != want {
		t.Fatalf("after hiding u_two and the reorder: %q, want %q", got, want)
	}
	if got, want := state(t, db, mtproto.PEER_CHANNEL, id), "c_main+ c_one+"; got != want {
		t.Errorf("the channel of the same id: %q, want %q", got, want)
	}
}

// Two editors showing the tenth and the eleventh username at once: Load
// locks the rows, so the second sees the first's change and only one of
// them is shown.
func TestToggleRace(t *testing.T) {
	db := startDB(t)
	const channelId = int64(4200)
	names := []string{"r_main"}
	for i := 1; i < MaxShown-1; i++ {
		names = append(names, fmt.Sprintf("r_shown%d", i))
	}
	names = append(names, "r_a", "r_b")
	seed(t, db, mtproto.PEER_CHANNEL, channelId, names, names[1:MaxShown-1]...)

	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	for i, name := range []string{"r_a", "r_b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = edit(t, db, int64(4201+i), mtproto.PEER_CHANNEL, channelId, func(b *userbox.Box, rows []Row) error {
				return Toggle(b, rows, name, true)
			})
		}()
	}
	wg.Wait()
	var ok, tooMany int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrTooMany):
			tooMany++
		default:
			t.Errorf("toggle: %v", err)
		}
	}
	if ok != 1 || tooMany != 1 {
		t.Errorf("%d shown, %d ErrTooMany; want one each", ok, tooMany)
	}
	if got := strings.Count(state(t, db, mtproto.PEER_CHANNEL, channelId), "+"); got != MaxShown {
		t.Errorf("%d shown, want %d", got, MaxShown)
	}
}
