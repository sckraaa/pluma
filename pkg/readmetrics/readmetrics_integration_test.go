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

package readmetrics

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The tables of migrate-20261119-read-metrics.sql on the production MySQL.

const migration = "migrate-20261119-read-metrics.sql"

var itMySQL *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

// itSeq makes ids unique across tests and runs (go test -count=N).
var itSeq atomic.Int64

func itId() int64 {
	return time.Now().UnixMilli()%1_000_000_000*1000 + itSeq.Add(10)
}

func itStore(at time.Time) *Store {
	c := itMySQL.Config()
	s := New(sqlx.NewMySQL(&c))
	s.now = func() time.Time { return at }
	return s
}

func count(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n []struct {
		N int `db:"n"`
	}
	if err := s.db.QueryRowsPartial(context.Background(), &n, q, args...); err != nil || len(n) != 1 {
		t.Fatalf("%s: %v, %v", q, n, err)
	}
	return n[0].N
}

// The file runs again on a database that has the tables and their rows,
// as the migrate service runs every file on each start.
func TestMigrationAgain(t *testing.T) {
	ctx := context.Background()
	s := itStore(time.Now())
	user, channel := itId(), itId()
	if _, err := s.AddMetrics(ctx, user, mtproto.PEER_CHANNEL, channel, []Metric{{MsgId: 1, ViewId: 1}}); err != nil {
		t.Fatal(err)
	}
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = itMySQL.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("the migration again: %v", err)
		}
	}
	if n := count(t, s, "select count(*) n from message_read_metrics where user_id = ?", user); n != 1 {
		t.Errorf("rows after the migration ran again: %d", n)
	}
}

func TestAddMetricsKeepsAShowingOnce(t *testing.T) {
	ctx := context.Background()
	s := itStore(time.Unix(1_800_000_000, 0))
	user, channel := itId(), itId()
	batch := []Metric{
		{MsgId: 5, ViewId: 100, TimeInViewMs: 1500, ActiveTimeInViewMs: 900, HeightToViewportRatioPermille: 2400, SeenRangeRatioPermille: 1000},
		{MsgId: 6, ViewId: 101, TimeInViewMs: 300},
		{MsgId: 6, ViewId: 101, TimeInViewMs: 300}, // the same showing twice in a batch
	}
	n, err := s.AddMetrics(ctx, user, mtproto.PEER_CHANNEL, channel, batch)
	if err != nil || n != 2 {
		t.Fatalf("first batch: %d rows, %v", n, err)
	}
	// The batch sent again (a retry) adds nothing; a new showing is added.
	n, err = s.AddMetrics(ctx, user, mtproto.PEER_CHANNEL, channel, append(batch, Metric{MsgId: 5, ViewId: 102, TimeInViewMs: 700}))
	if err != nil || n != 1 {
		t.Fatalf("again with a new showing: %d rows, %v", n, err)
	}
	// Another reader's showing with the same view_id is theirs.
	other := itId()
	if n, err = s.AddMetrics(ctx, other, mtproto.PEER_CHANNEL, channel, batch[:1]); err != nil || n != 1 {
		t.Fatalf("another reader: %d rows, %v", n, err)
	}
	var row []struct {
		Height int32 `db:"height_to_viewport_ratio_permille"`
		Date   int64 `db:"date"`
	}
	if err = s.db.QueryRowsPartial(ctx, &row,
		"select height_to_viewport_ratio_permille, date from message_read_metrics where peer_type = ? and peer_id = ? and msg_id = 5 and user_id = ? and view_id = 100",
		mtproto.PEER_CHANNEL, channel, user); err != nil || len(row) != 1 || row[0].Height != 2400 || row[0].Date != 1_800_000_000 {
		t.Fatalf("the stored row: %+v, %v", row, err)
	}

	stats, err := s.Aggregate(ctx, mtproto.PEER_CHANNEL, channel, []int32{5, 6, 7})
	if err != nil {
		t.Fatal(err)
	}
	want := []MessageStats{
		{MsgId: 5, Views: 3, Readers: 2, TimeInViewMsSum: 3700, ActiveTimeInViewMsSum: 1800, TimeInViewMsMedian: 1500, ActiveTimeInViewMsMedian: 900, SeenRangePermilleMedian: 1000},
		{MsgId: 6, Views: 1, Readers: 1, TimeInViewMsSum: 300, TimeInViewMsMedian: 300},
		{MsgId: 7},
	}
	for i := range want {
		if stats[i] != want[i] {
			t.Errorf("stats %d: %+v, want %+v", i, stats[i], want[i])
		}
	}
}

// A full batch goes in one statement.
func TestAddMetricsFullBatch(t *testing.T) {
	ctx := context.Background()
	s := itStore(time.Now())
	user, chat := itId(), itId()
	batch := make([]Metric, MaxMetrics)
	for i := range batch {
		batch[i] = Metric{MsgId: int32(i + 1), ViewId: int64(i), TimeInViewMs: 10}
	}
	if n, err := s.AddMetrics(ctx, user, mtproto.PEER_CHAT, chat, batch); err != nil || n != MaxMetrics {
		t.Fatalf("a full batch: %d rows, %v", n, err)
	}
}

func TestAddListenOneRowAMinute(t *testing.T) {
	ctx := context.Background()
	base := time.Unix(1_800_000_000/ListenWindow*ListenWindow, 0) // the start of a minute
	user, doc := itId(), itId()
	if err := itStore(base.Add(5*time.Second)).AddListen(ctx, user, doc, 30); err != nil {
		t.Fatal(err)
	}
	// Concurrent listens of the same minute make one row with the longest.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := itStore(base.Add(time.Duration(10+i)*time.Second)).AddListen(ctx, user, doc, int32(40+i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s := itStore(base)
	var rows []struct {
		Duration int32 `db:"listened_duration"`
		Date     int64 `db:"date"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "select listened_duration, date from music_listens where user_id = ? and document_id = ?", user, doc); err != nil ||
		len(rows) != 1 || rows[0].Duration != 47 || rows[0].Date != base.Unix()+5 {
		t.Fatalf("one minute: %+v, %v", rows, err)
	}
	// The next minute is a row of its own.
	if err := itStore(base.Add(ListenWindow*time.Second)).AddListen(ctx, user, doc, 12); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "select count(*) n from music_listens where user_id = ? and document_id = ?", user, doc); n != 2 {
		t.Errorf("two minutes: %d rows", n)
	}
}

func TestDeleteUser(t *testing.T) {
	ctx := context.Background()
	s := itStore(time.Now())
	gone, stays, channel, doc := itId(), itId(), itId(), itId()
	for _, u := range []int64{gone, stays} {
		if _, err := s.AddMetrics(ctx, u, mtproto.PEER_CHANNEL, channel, []Metric{{MsgId: 1, ViewId: 1}}); err != nil {
			t.Fatal(err)
		}
		if err := s.AddListen(ctx, u, doc, 10); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteUser(ctx, gone); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user int64
		want int
	}{{gone, 0}, {stays, 1}} {
		if n := count(t, s, "select count(*) n from message_read_metrics where user_id = ?", tc.user); n != tc.want {
			t.Errorf("metrics of user %d: %d, want %d", tc.user, n, tc.want)
		}
		if n := count(t, s, "select count(*) n from music_listens where user_id = ?", tc.user); n != tc.want {
			t.Errorf("listens of user %d: %d, want %d", tc.user, n, tc.want)
		}
	}
}
