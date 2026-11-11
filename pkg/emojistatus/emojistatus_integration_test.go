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

package emojistatus

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The recent statuses of an account (migrate-20261114-user-look.sql): the
// newest first, at most RecentLimit, the oldest dropped; a status that
// already heads the list changes nothing; clearing tells the devices once
// and an empty list nobody.
func TestRecent(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	boxDB, err := userbox.Open(db.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boxDB.Close() })
	if err := outbox.Register(outbox.Topics{Push: []string{"Sync-T"}}); err != nil {
		t.Fatal(err)
	}
	s := NewStore(sqlx.NewMySQL(&conf), boxDB, "Sync-T")

	const user, other = int64(7), int64(8)
	use := func(userID, id int64) bool {
		t.Helper()
		var listed bool
		if _, err := userbox.Run(ctx, boxDB, userID, func(b *userbox.Box) error {
			var err error
			listed, err = Use(b, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return listed
	}
	ids := func(userID int64) []int64 {
		t.Helper()
		rows, err := s.Recent(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.DocumentID
			if r.UsedAt == 0 {
				t.Errorf("status %d used at 0", r.DocumentID)
			}
		}
		return out
	}
	journal := func(userID int64) []string {
		t.Helper()
		return seqKinds(t, boxDB, userID)
	}

	for _, id := range []int64{1, 2, 3} {
		if !use(user, id) {
			t.Errorf("status %d did not change the list", id)
		}
	}
	use(other, 50)
	if use(user, 3) {
		t.Error("the head of the list again changed the list")
	}
	if !use(user, 1) {
		t.Error("an older status did not move to the head")
	}
	if got := ids(user); !slices.Equal(got, []int64{1, 3, 2}) {
		t.Fatalf("recent %v, want [1 3 2]", got)
	}
	for id := int64(100); id < 100+RecentLimit; id++ {
		use(user, id)
	}
	got := ids(user)
	if len(got) != RecentLimit || got[0] != 100+RecentLimit-1 || got[RecentLimit-1] != 100 {
		t.Fatalf("%d recent statuses from %d to %d, want %d from %d to 100", len(got), got[0], got[len(got)-1], RecentLimit, 100+RecentLimit-1)
	}
	// Use writes no update by itself: the caller does.
	if j := journal(user); len(j) != 0 {
		t.Fatalf("journal %v after Use alone", j)
	}

	if err = s.Clear(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err = s.Clear(ctx, user); err != nil { // nothing to clear
		t.Fatal(err)
	}
	if got = ids(user); len(got) != 0 {
		t.Errorf("recent %v after the clear", got)
	}
	if j := journal(user); !slices.Equal(j, []string{"updateRecentEmojiStatuses"}) {
		t.Errorf("journal %v, want one updateRecentEmojiStatuses", j)
	}
	if got = ids(other); !slices.Equal(got, []int64{50}) {
		t.Errorf("the other account has %v, want [50]", got)
	}
}

// seqKinds are the kinds of the updates with seq of the account, in order;
// each goes to every device, the one that made the change too.
func seqKinds(t *testing.T, boxDB *sql.DB, userID int64) []string {
	t.Helper()
	rows, err := boxDB.Query("SELECT update_type, except_auth_key_id FROM user_seq_updates WHERE user_id = ? ORDER BY seq", userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var (
			kind   string
			except int64
		)
		if err = rows.Scan(&kind, &except); err != nil {
			t.Fatal(err)
		}
		if except != 0 {
			t.Errorf("%s kept from the device that made it (except %d)", kind, except)
		}
		kinds = append(kinds, kind)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return kinds
}
