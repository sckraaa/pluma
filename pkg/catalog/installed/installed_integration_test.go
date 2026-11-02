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

package installed

import (
	"context"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// journaled is a row of the account's seq journal.
type journaled struct {
	kind   string
	except int64
}

// Sets of an account (migrate-20261103-user-sticker-sets.sql): each kind in
// its own order, the last installed first; archived sets apart, the last
// archived first; each change one update with seq for the other devices, and
// a change that changes nothing no update at all.
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

	const (
		user = int64(7)
		key  = int64(70)
	)
	newSet := func(id int64) func(int64) *mtproto.Update {
		return func(installedAt int64) *mtproto.Update {
			if installedAt == 0 {
				t.Errorf("set %d installed at 0", id)
			}
			return mtproto.MakeTLUpdateNewStickerSet(&mtproto.Update{
				Stickerset_MESSAGES_STICKERSET: mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
					Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: id, InstalledDate: mtproto.MakeFlagsInt32(int32(installedAt))}).To_StickerSet(),
				}).To_Messages_StickerSet(),
			}).To_Update()
		}
	}
	install := func(kind Kind, id int64, archived bool) {
		t.Helper()
		if err := s.Install(ctx, user, key, kind, id, archived, newSet(id)); err != nil {
			t.Fatalf("install %d: %v", id, err)
		}
	}
	ids := func(rows []Entry, err error) []int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.SetID
		}
		return out
	}
	installedOf := func(kind Kind) []int64 { return ids(s.Installed(ctx, user, kind)) }
	archivedOf := func(kind Kind) []int64 { return ids(s.Archived(ctx, user, kind)) }
	seen := 0
	journal := func() []journaled {
		t.Helper()
		rows, err := boxDB.Query("SELECT update_type, except_auth_key_id FROM user_seq_updates WHERE user_id = ? ORDER BY seq", user)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []journaled
		for rows.Next() {
			var j journaled
			if err := rows.Scan(&j.kind, &j.except); err != nil {
				t.Fatal(err)
			}
			out = append(out, j)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		fresh := out[seen:]
		seen = len(out)
		return fresh
	}
	expect := func(step string, kinds ...string) {
		t.Helper()
		got := journal()
		var names []string
		for _, j := range got {
			names = append(names, j.kind)
			if j.except != key {
				t.Errorf("%s: %s sent to the device that made it (except %d)", step, j.kind, j.except)
			}
		}
		if !slices.Equal(names, kinds) {
			t.Fatalf("%s: journal %v, want %v", step, names, kinds)
		}
	}

	for _, id := range []int64{1, 2, 3} {
		install(Stickers, id, false)
	}
	install(Emojis, 10, false)
	expect("installs", "updateNewStickerSet", "updateNewStickerSet", "updateNewStickerSet", "updateNewStickerSet")
	if got := installedOf(Stickers); !slices.Equal(got, []int64{3, 2, 1}) {
		t.Fatalf("stickers %v", got)
	}
	if got := installedOf(Emojis); !slices.Equal(got, []int64{10}) {
		t.Fatalf("emojis %v", got)
	}

	install(Stickers, 3, false) // at the top already
	expect("the top again")
	install(Stickers, 1, false) // to the top
	expect("to the top", "updateNewStickerSet")
	if got := installedOf(Stickers); !slices.Equal(got, []int64{1, 3, 2}) {
		t.Fatalf("stickers after a move %v", got)
	}

	// Archived: off the list; archived again, nothing; unarchived, on top.
	if err := s.Toggle(ctx, user, key, Archive, []Ref{{SetID: 3, Kind: Stickers}, {SetID: 10, Kind: Emojis}, {SetID: 99, Kind: Masks}}); err != nil {
		t.Fatal(err)
	}
	expect("archive", "updateStickerSets", "updateStickerSets", "updateStickerSets")
	if got := installedOf(Stickers); !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("stickers after archive %v", got)
	}
	if got := archivedOf(Masks); !slices.Equal(got, []int64{99}) {
		t.Fatalf("archived masks %v", got)
	}
	if err := s.Toggle(ctx, user, key, Archive, []Ref{{SetID: 3, Kind: Stickers}}); err != nil {
		t.Fatal(err)
	}
	expect("archive again")
	install(Stickers, 2, true) // install archived
	expect("install archived", "updateStickerSets")
	if got := archivedOf(Stickers); !slices.Equal(got, []int64{2, 3}) && !slices.Equal(got, []int64{3, 2}) {
		t.Fatalf("archived stickers %v", got)
	}
	if err := s.Toggle(ctx, user, key, Unarchive, []Ref{{SetID: 3, Kind: Stickers}}); err != nil {
		t.Fatal(err)
	}
	expect("unarchive", "updateStickerSets")
	if got := installedOf(Stickers); !slices.Equal(got, []int64{3, 1}) {
		t.Fatalf("stickers after unarchive %v", got)
	}

	// Reorder: unknown and repeated ids dropped, the left out appended.
	install(Stickers, 4, false)
	expect("one more", "updateNewStickerSet")
	stored, err := s.Reorder(ctx, user, key, Stickers, []int64{1, 1, 77, 4})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 4, 3}; !slices.Equal(stored, want) || !slices.Equal(installedOf(Stickers), want) {
		t.Fatalf("reorder stored %v, installed %v, want %v", stored, installedOf(Stickers), want)
	}
	expect("reorder", "updateStickerSetsOrder")
	if _, err := s.Reorder(ctx, user, key, Stickers, []int64{1, 4, 3}); err != nil {
		t.Fatal(err)
	}
	expect("the same order")

	// Uninstalled, installed or archived: gone; gone already, nothing.
	if err := s.Toggle(ctx, user, key, Uninstall, []Ref{{SetID: 4, Kind: Stickers}, {SetID: 2, Kind: Stickers}, {SetID: 5, Kind: Stickers}}); err != nil {
		t.Fatal(err)
	}
	expect("uninstall", "updateStickerSets")
	if got, arch := installedOf(Stickers), archivedOf(Stickers); !slices.Equal(got, []int64{1, 3}) || len(arch) != 0 {
		t.Fatalf("after uninstall: installed %v, archived %v", got, arch)
	}
	if err := s.Toggle(ctx, user, key, Uninstall, []Ref{{SetID: 4, Kind: Stickers}}); err != nil {
		t.Fatal(err)
	}
	expect("uninstall again")
}
