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

package saved

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/ringtones"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The saved sounds of an account (migrate-20261114-user-look.sql): the
// newest first, each once, at most ringtones.SavedCountMax; each change is
// one updateSavedRingtones with seq for every device, and a save or an
// unsave that changes nothing writes none.
func TestSaved(t *testing.T) {
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
	ids := func(userID int64) []int64 {
		t.Helper()
		rows, err := s.Saved(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.DocumentID
		}
		return out
	}
	updates := func(userID int64) int {
		t.Helper()
		var n, kept int
		if err := boxDB.QueryRow("SELECT COUNT(*), COALESCE(SUM(except_auth_key_id <> 0), 0) FROM user_seq_updates WHERE user_id = ? AND update_type = 'updateSavedRingtones'", userID).Scan(&n, &kept); err != nil {
			t.Fatal(err)
		}
		if kept != 0 {
			t.Errorf("%d updates kept from the device that made the change", kept)
		}
		return n
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(s.Save(ctx, user, 1))
	must(s.Save(ctx, user, 2))
	must(s.Save(ctx, user, 1)) // saved already: stays where it is
	must(s.Save(ctx, other, 9))
	if got := ids(user); !slices.Equal(got, []int64{2, 1}) {
		t.Fatalf("saved %v, want [2 1]", got)
	}
	if n := updates(user); n != 2 {
		t.Errorf("%d updates after two saves and a repeat, want 2", n)
	}
	must(s.Unsave(ctx, user, 5)) // not saved
	must(s.Unsave(ctx, user, 2))
	if got := ids(user); !slices.Equal(got, []int64{1}) {
		t.Fatalf("saved %v after the unsave, want [1]", got)
	}
	if n := updates(user); n != 3 {
		t.Errorf("%d updates, want 3", n)
	}

	for id := int64(100); len(ids(user)) < ringtones.SavedCountMax; id++ {
		must(s.Save(ctx, user, id))
	}
	before := updates(user)
	if err = s.Save(ctx, user, 5000); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a sound over the limit: %v, want ErrTooMany", err)
	}
	must(s.Save(ctx, user, 1)) // one of the list is still accepted
	if got := ids(user); len(got) != ringtones.SavedCountMax || slices.Contains(got, 5000) || updates(user) != before {
		t.Errorf("%d saved, %d updates after the refusal; want %d and %d", len(got), updates(user), ringtones.SavedCountMax, before)
	}
	if got := ids(other); !slices.Equal(got, []int64{9}) {
		t.Errorf("the other account has %v, want [9]", got)
	}
}
