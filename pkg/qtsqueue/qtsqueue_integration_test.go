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

package qtsqueue

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

var itDB *sql.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	my, err := testinfra.StartMySQL(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if itDB, err = userbox.Open(my.DSN()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = my.Terminate(ctx)
		os.Exit(1)
	}
	code := m.Run()
	_ = itDB.Close()
	_ = my.Terminate(ctx)
	os.Exit(code)
}

var deviceSeq struct {
	sync.Mutex
	n int64
}

// newDevice is a signed-in authorization of a new account.
func newDevice(t *testing.T) Device {
	t.Helper()
	deviceSeq.Lock()
	deviceSeq.n++
	d := Device{UserId: 50_000_000 + deviceSeq.n, AuthKey: 60_000_000 + deviceSeq.n}
	deviceSeq.Unlock()
	if _, err := itDB.Exec("INSERT INTO auth_users (auth_key_id, user_id, hash, deleted, date_created) VALUES (?, ?, ?, 0, ?)",
		d.AuthKey, d.UserId, d.AuthKey, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	return d
}

func deleteUpdate(id int32) *mtproto.Update {
	return mtproto.MakeTLUpdateBotDeleteBusinessMessage(&mtproto.Update{
		ConnectionId: "c",
		Peer_PEER:    mtproto.MakePeerUser(7),
		Messages:     []int32{id},
	}).To_Update()
}

// appendAt queues the update of message id for d at date, in its own
// transaction.
func appendAt(t *testing.T, d Device, id int32, date int32) int32 {
	t.Helper()
	ctx := context.Background()
	tx, err := itDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	qts, err := Append(SQLTx(ctx, tx), d, deleteUpdate(id), []int64{7, 8}, date)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return qts
}

func now() int32 { return int32(time.Now().Unix()) }

func qtsOf(us []*Update) []int32 {
	out := make([]int32, len(us))
	for i, u := range us {
		out[i] = u.Qts
	}
	return out
}

// The numbers follow the counter from FirstQts on, the update carries its
// own, and a difference gives the queue after the client's qts, which it
// confirms: the next one from there has nothing.
func TestAppendAndDifference(t *testing.T) {
	ctx := context.Background()
	s := New(itDB)
	d := newDevice(t)
	if qts, err := s.Current(ctx, d); err != nil || qts != FirstQts {
		t.Fatalf("a new authorization at %d, %v", qts, err)
	}
	for i := range 3 {
		if qts := appendAt(t, d, int32(i+1), now()); qts != FirstQts+1+int32(i) {
			t.Fatalf("update %d with qts %d", i, qts)
		}
	}
	top, err := s.Current(ctx, d)
	if err != nil || top != FirstQts+3 {
		t.Fatalf("counter %d, %v", top, err)
	}
	diff, err := s.Difference(ctx, d, FirstQts, top)
	if err != nil || diff.More || !slices.Equal(qtsOf(diff.Updates), []int32{FirstQts + 1, FirstQts + 2, FirstQts + 3}) {
		t.Fatalf("difference %v (more %v), %v", qtsOf(diff.Updates), diff.More, err)
	}
	u := diff.Updates[0]
	if u.Update.GetQts() != u.Qts || u.Update.GetPredicateName() != mtproto.Predicate_updateBotDeleteBusinessMessage ||
		u.Update.GetMessages()[0] != 1 || !slices.Equal(u.UserIds, []int64{7, 8}) {
		t.Fatalf("the first update: %+v", u)
	}
	// Read up to an earlier counter: the rows past it wait.
	if diff, err = s.Difference(ctx, d, FirstQts+1, FirstQts+2); err != nil || !slices.Equal(qtsOf(diff.Updates), []int32{FirstQts + 2}) {
		t.Fatalf("up to %d: %v, %v", FirstQts+2, qtsOf(diff.Updates), err)
	}
	if diff, err = s.Difference(ctx, d, top, top); err != nil || len(diff.Updates) != 0 {
		t.Fatalf("after the confirmation: %v, %v", qtsOf(diff.Updates), err)
	}
	var n int
	if err = itDB.QueryRow("SELECT COUNT(*) FROM qts_updates WHERE auth_key_id = ? AND user_id = ?", d.AuthKey, d.UserId).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d rows left after the confirmation, %v", n, err)
	}
	// A client without a qts (-1) confirms nothing and gets all.
	appendAt(t, d, 9, now())
	if diff, err = s.Difference(ctx, d, -1, top+1); err != nil || !slices.Equal(qtsOf(diff.Updates), []int32{top + 1}) {
		t.Fatalf("from -1: %v, %v", qtsOf(diff.Updates), err)
	}
}

// Two writers of one authorization get consecutive numbers: the counter's
// row orders them.
func TestConcurrentAppend(t *testing.T) {
	d := newDevice(t)
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		got []int32
	)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			qts := appendAt(t, d, int32(i), now())
			mu.Lock()
			got = append(got, qts)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.Sort(got)
	for i, qts := range got {
		if qts != FirstQts+1+int32(i) {
			t.Fatalf("numbers %v", got)
		}
	}
}

// A page holds PageSize updates; the next one goes on from its last qts.
func TestDifferencePages(t *testing.T) {
	ctx := context.Background()
	s := New(itDB)
	d := newDevice(t)
	for i := range PageSize + 5 {
		appendAt(t, d, int32(i), now())
	}
	top, err := s.Current(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Difference(ctx, d, FirstQts, top)
	if err != nil || len(first.Updates) != PageSize || !first.More {
		t.Fatalf("first page: %d (more %v), %v", len(first.Updates), first.More, err)
	}
	last := first.Updates[PageSize-1].Qts
	second, err := s.Difference(ctx, d, last, top)
	if err != nil || len(second.Updates) != 5 || second.More || second.Updates[0].Qts != last+1 {
		t.Fatalf("second page: %v (more %v), %v", qtsOf(second.Updates), second.More, err)
	}
}

// Over the limit the oldest updates go; the sweep drops those past the
// retention and those older than a later login on the same key, and
// Forget the whole authorization.
func TestLimitAndSweep(t *testing.T) {
	ctx := context.Background()
	s := New(itDB)
	saved := deviceLimit
	deviceLimit = 3
	t.Cleanup(func() { deviceLimit = saved })

	d := newDevice(t)
	for i := range 5 {
		appendAt(t, d, int32(i), now())
	}
	top, _ := s.Current(ctx, d)
	diff, err := s.Difference(ctx, d, -1, top)
	if err != nil || !slices.Equal(qtsOf(diff.Updates), []int32{top - 2, top - 1, top}) {
		t.Fatalf("over the limit: %v, %v", qtsOf(diff.Updates), err)
	}

	old := newDevice(t)
	appendAt(t, old, 1, int32(time.Now().Add(-Retention-time.Hour).Unix()))
	fresh := appendAt(t, old, 2, now())
	relogged := newDevice(t)
	appendAt(t, relogged, 1, now()-60)
	if _, err = itDB.Exec("UPDATE auth_users SET date_created = ? WHERE auth_key_id = ? AND user_id = ?", now(), relogged.AuthKey, relogged.UserId); err != nil {
		t.Fatal(err)
	}
	if err = Sweep(ctx, itDB, time.Now()); err != nil {
		t.Fatal(err)
	}
	if diff, err = s.Difference(ctx, old, -1, fresh); err != nil || !slices.Equal(qtsOf(diff.Updates), []int32{fresh}) {
		t.Fatalf("after the sweep: %v, %v", qtsOf(diff.Updates), err)
	}
	if diff, err = s.Difference(ctx, relogged, -1, FirstQts+1); err != nil || len(diff.Updates) != 0 {
		t.Fatalf("an update of the earlier login: %v, %v", qtsOf(diff.Updates), err)
	}

	if err = Forget(ctx, itDB, old); err != nil {
		t.Fatal(err)
	}
	if qts, err := s.Current(ctx, old); err != nil || qts != FirstQts {
		t.Fatalf("a forgotten authorization at %d, %v", qts, err)
	}
	if diff, err = s.Difference(ctx, old, -1, fresh); err != nil || len(diff.Updates) != 0 {
		t.Fatalf("the queue of a forgotten authorization: %v, %v", qtsOf(diff.Updates), err)
	}
}
