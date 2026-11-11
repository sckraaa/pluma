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

// A set a user made changes for everyone who has it (task 084): each of
// their accounts is told to load its sets of the kind again, and a deleted
// set leaves them all. The trending sets an account saw are kept, and its
// other devices told once per kind.
func TestSharedSetsAndFeatured(t *testing.T) {
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
	whole := func(int64) *mtproto.Update {
		return mtproto.MakeTLUpdateNewStickerSet(&mtproto.Update{
			Stickerset_MESSAGES_STICKERSET: mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
				Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 50}).To_StickerSet(),
			}).To_Messages_StickerSet(),
		}).To_Update()
	}
	type row struct {
		kind   string
		except int64
	}
	journal := func(user int64) []row {
		t.Helper()
		rows, err := boxDB.Query("SELECT update_type, except_auth_key_id FROM user_seq_updates WHERE user_id = ? ORDER BY seq", user)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.kind, &r.except); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	kinds := func(user int64) []string {
		var out []string
		for _, r := range journal(user) {
			out = append(out, r.kind)
		}
		return out
	}

	if err := s.Install(ctx, 7, Stickers, 50, false, whole); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(ctx, 8, Stickers, 50, true, whole); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(ctx, 8, Stickers, 60, false, whole); err != nil {
		t.Fatal(err)
	}
	if holders, err := s.Holders(ctx, 50); err != nil || !slices.Equal(holders, []int64{7, 8}) {
		t.Fatalf("Holders: %v, %v", holders, err)
	}
	before7, before8, before9 := len(journal(7)), len(journal(8)), len(journal(9))
	if err := s.Changed(ctx, 50, Stickers); err != nil {
		t.Fatal(err)
	}
	if got := kinds(7)[before7:]; !slices.Equal(got, []string{mtproto.Predicate_updateStickerSets}) {
		t.Errorf("an account that installed the changed set got %v", got)
	}
	if got := kinds(8)[before8:]; !slices.Equal(got, []string{mtproto.Predicate_updateStickerSets}) {
		t.Errorf("an account that archived the changed set got %v", got)
	}
	if len(journal(9)) != before9 {
		t.Errorf("an account without the set was told of its change")
	}

	before7, before8 = len(journal(7)), len(journal(8))
	if err := s.Drop(ctx, 50, Stickers); err != nil {
		t.Fatal(err)
	}
	if holders, _ := s.Holders(ctx, 50); len(holders) != 0 {
		t.Errorf("accounts still have the dropped set: %v", holders)
	}
	if e, _ := s.Entry(ctx, 8, 60); e == nil {
		t.Errorf("another set of the account went with the dropped one")
	}
	if a, b := kinds(7)[before7:], kinds(8)[before8:]; !slices.Equal(a, []string{mtproto.Predicate_updateStickerSets}) || !slices.Equal(b, a) {
		t.Errorf("the accounts that lost the set got %v and %v", a, b)
	}

	// The trending sets seen.
	const device = int64(4242)
	before7 = len(journal(7))
	if err := s.ReadFeatured(ctx, 7, device, []int64{1, 2}, []int64{3}); err != nil {
		t.Fatal(err)
	}
	read, err := s.FeaturedRead(ctx, 7)
	if err != nil || len(read) != 3 || !read[1] || !read[2] || !read[3] {
		t.Fatalf("FeaturedRead: %v, %v", read, err)
	}
	got := journal(7)[before7:]
	if len(got) != 2 || got[0] != (row{mtproto.Predicate_updateReadFeaturedStickers, device}) || got[1] != (row{mtproto.Predicate_updateReadFeaturedEmojiStickers, device}) {
		t.Errorf("the account's other devices got %v", got)
	}
	before7 = len(journal(7))
	if err := s.ReadFeatured(ctx, 7, device, []int64{2, 4}, []int64{3}); err != nil {
		t.Fatal(err)
	}
	if got := kinds(7)[before7:]; !slices.Equal(got, []string{mtproto.Predicate_updateReadFeaturedStickers}) {
		t.Errorf("reading one more sticker set and an emoji set read before: %v", got)
	}
	before7 = len(journal(7))
	if err := s.ReadFeatured(ctx, 7, device, []int64{1, 2, 4}, nil); err != nil {
		t.Fatal(err)
	}
	if len(journal(7)) != before7 {
		t.Errorf("reading what is read told the devices again")
	}
	if read, _ := s.FeaturedRead(ctx, 8); len(read) != 0 {
		t.Errorf("another account's read sets: %v", read)
	}
}
