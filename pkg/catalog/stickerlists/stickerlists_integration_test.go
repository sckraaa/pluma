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

package stickerlists

import (
	"context"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Lists of an account (migrate-20261111-user-sticker-lists.sql): each the
// newest first and apart from the others, at most its limit, the oldest
// dropped; each change one update with seq for every device, the one that
// made it too, and a change that changes nothing no update at all.
func TestStore(t *testing.T) {
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

	const user = int64(7)
	ids := func(list List) []int64 {
		t.Helper()
		rows, err := s.Entries(ctx, user, list)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.DocumentID
			if r.UsedAt == 0 {
				t.Errorf("sticker %d used at 0", r.DocumentID)
			}
		}
		return out
	}
	seen := 0
	expect := func(step string, kinds ...string) {
		t.Helper()
		rows, err := boxDB.Query("SELECT update_type, except_auth_key_id FROM user_seq_updates WHERE user_id = ? ORDER BY seq", user)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var kind string
			var except int64
			if err := rows.Scan(&kind, &except); err != nil {
				t.Fatal(err)
			}
			if except != 0 {
				t.Errorf("%s: %s kept from the device that made it (except %d)", step, kind, except)
			}
			names = append(names, kind)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		fresh := names[seen:]
		seen = len(names)
		if !slices.Equal(fresh, kinds) {
			t.Fatalf("%s: journal %v, want %v", step, fresh, kinds)
		}
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, id := range []int64{1, 2, 3} {
		must(s.Add(ctx, user, Recent, id))
	}
	must(s.Add(ctx, user, RecentAttached, 9))
	expect("adds", "updateRecentStickers", "updateRecentStickers", "updateRecentStickers", "updateRecentStickers")
	if got := ids(Recent); !slices.Equal(got, []int64{3, 2, 1}) {
		t.Fatalf("recent %v", got)
	}
	if got := ids(RecentAttached); !slices.Equal(got, []int64{9}) {
		t.Fatalf("attached %v", got)
	}

	must(s.Add(ctx, user, Recent, 3))
	expect("the top again")
	must(s.Add(ctx, user, Recent, 1))
	expect("to the top", "updateRecentStickers")
	if got := ids(Recent); !slices.Equal(got, []int64{1, 3, 2}) {
		t.Fatalf("recent %v", got)
	}

	must(s.Remove(ctx, user, Recent, 5))
	expect("remove of none")
	must(s.Remove(ctx, user, Recent, 3))
	expect("remove", "updateRecentStickers")
	if got := ids(Recent); !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("recent %v", got)
	}

	for id := int64(100); id < 100+FavedLimit+2; id++ {
		must(s.Add(ctx, user, Faved, id))
	}
	if got, want := ids(Faved), []int64{106, 105, 104, 103, 102}; !slices.Equal(got, want) {
		t.Fatalf("faved %v, want %v: the oldest past the limit go", got, want)
	}
	expect("faves", slices.Repeat([]string{"updateFavedStickers"}, FavedLimit+2)...)

	must(s.Clear(ctx, user, Recent))
	expect("clear", "updateRecentStickers")
	must(s.Clear(ctx, user, Recent))
	expect("clear of none")
	if got := ids(Recent); len(got) != 0 {
		t.Fatalf("recent %v after clear", got)
	}
	if got := ids(RecentAttached); !slices.Equal(got, []int64{9}) {
		t.Fatalf("attached %v after clear of recent", got)
	}
}
