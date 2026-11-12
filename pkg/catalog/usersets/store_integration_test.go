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

package usersets

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"google.golang.org/protobuf/proto"
)

func decode(t *testing.T, set *catalog.Set) *mtproto.Messages_StickerSet {
	t.Helper()
	ms := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(set.Data, ms); err != nil {
		t.Fatal(err)
	}
	return ms
}

// The sets users make live in the catalog's table
// (migrate-20261114-own-sticker-sets.sql): found by id and by short name
// as any set; one short name, in any case, for one set; changed in one
// transaction with the stickers that name their set; kept by an import of
// the catalog and by a set fetched on demand that has their name; and
// their stickers, those taken out too, are documents anyone may fetch.
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	s := NewStore(sqlx.NewMySQL(&conf))
	cat := catalog.NewStore(sqlx.NewMySQL(&conf))

	ms := New(500, 501, "Cats", "MyCats", installed.Stickers, false)
	Insert(ms, 0, Sticker{Document: doc(1001), Emoji: []string{"😺"}})
	set, err := s.Create(ctx, 7, ms, 0, []Link{{DocumentID: 1001, SourceID: 11}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cat.SetByShortName(ctx, "mycats")
	if err != nil || got == nil || got.ID != 500 || got.OwnerID != 7 || got.Hash != set.Hash || got.Hash == 0 || decode(t, got).GetSet().GetCount() != 1 {
		t.Fatalf("the set by its short name: %+v, %v", got, err)
	}
	if _, err := s.Create(ctx, 8, New(600, 601, "Other", "mycats", installed.Stickers, false), 0, nil, 100); !errors.Is(err, ErrShortNameOccupied) {
		t.Fatalf("a second set of the short name: %v", err)
	}
	if taken, err := s.ShortNameTaken(ctx, "MYCATS"); err != nil || !taken {
		t.Errorf("ShortNameTaken of a taken name: %v, %v", taken, err)
	}
	if taken, err := s.ShortNameTaken(ctx, "mycats2"); err != nil || taken {
		t.Errorf("ShortNameTaken of a free name: %v, %v", taken, err)
	}

	// A change stores the set and its stickers together.
	changed, err := s.Update(ctx, 500, func(c *Change) error {
		if c.Set.OwnerID != 7 || len(c.Links) != 1 || c.SourceOf(11) != 1001 || c.SourceOf(1001) != 1001 || c.SourceOf(12) != 0 {
			t.Errorf("the set being changed: %+v, links %v", c.Set, c.Links)
		}
		Insert(c.MS, 1, Sticker{Document: doc(1002), Emoji: []string{"🐶"}})
		c.Add(Link{DocumentID: 1002, SourceID: 12})
		Remove(c.MS, 1001)
		c.Drop(1001)
		c.Set.ThumbDocumentID = 77
		return nil
	})
	if err != nil || changed.Hash == set.Hash || changed.ThumbDocumentID != 77 {
		t.Fatalf("Update: %+v, %v", changed, err)
	}
	got, _ = cat.SetByID(ctx, 500)
	if d := decode(t, got).GetDocuments(); got.Hash != changed.Hash || len(d) != 1 || d[0].GetId() != 1002 || got.ThumbDocumentID != 77 {
		t.Fatalf("the stored set after the change: %+v", got)
	}
	if id, err := s.SetOf(ctx, 1002); err != nil || id != 500 {
		t.Errorf("SetOf a sticker of the set: %d, %v", id, err)
	}
	if id, err := s.SetOf(ctx, 1001); err != nil || id != 0 {
		t.Errorf("SetOf a sticker taken out: %d, %v", id, err)
	}
	if links, err := s.Links(ctx, 500); err != nil || len(links) != 1 || links[0] != (Link{DocumentID: 1002, SourceID: 12}) {
		t.Errorf("Links: %v, %v", links, err)
	}
	if ids, err := cat.CatalogDocuments(ctx, []int64{1002, 5, 1001}); err != nil || !slices.Equal(ids, []int64{1002, 1001}) {
		t.Errorf("the stickers of users' sets among the documents anyone may fetch: %v, %v", ids, err)
	}

	// A change that fails or changes nothing leaves the set as it was.
	boom := errors.New("boom")
	if _, err := s.Update(ctx, 500, func(c *Change) error {
		Remove(c.MS, 1002)
		c.Drop(1002)
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("a failed change: %v", err)
	}
	same, err := s.Update(ctx, 500, func(c *Change) error { c.MS.Set.Title = "Never"; return ErrUnchanged })
	if err != nil || same.Hash != changed.Hash {
		t.Fatalf("a change of nothing: %+v, %v", same, err)
	}
	if got, _ = cat.SetByID(ctx, 500); got.Hash != changed.Hash || decode(t, got).GetSet().GetTitle() != "Cats" {
		t.Errorf("the set after changes that did not happen: %+v", got)
	}
	if id, _ := s.SetOf(ctx, 1002); id != 500 {
		t.Errorf("a sticker dropped by a failed change")
	}
	if _, err := s.Update(ctx, 999, func(*Change) error { return nil }); !errors.Is(err, ErrNoSet) {
		t.Errorf("a change of no set: %v", err)
	}

	// The catalog and Telegram's sets never take the place of a user's.
	if err := cat.Replace(ctx, &catalog.Catalog{
		Answers: map[[2]string]catalog.Answer{{"messages.getAvailableReactions", ""}: {Hash: 1, Data: []byte("r")}},
		Sets: []*catalog.Set{
			{ID: 20, AccessHash: 21, ShortName: "HotCherry", Hash: 5, Data: []byte("set20")},
			{ID: 30, AccessHash: 31, ShortName: "mycats", Hash: 9, Data: []byte("set30")},
		},
		Now: 200,
	}); err != nil {
		t.Fatalf("an import of a catalog with a set named as a user's: %v", err)
	}
	if err := cat.PutSet(ctx, &catalog.Set{ID: 40, AccessHash: 41, ShortName: "MyCats", Hash: 3, Data: []byte("set40")}, 300); err != nil {
		t.Fatalf("a set fetched on demand named as a user's: %v", err)
	}
	if got, _ = cat.SetByShortName(ctx, "mycats"); got == nil || got.ID != 500 || got.OwnerID != 7 || got.Hash != changed.Hash || got.AccessHash != 501 {
		t.Errorf("the user's set after the import: %+v", got)
	}
	if got, _ = cat.SetByID(ctx, 20); got == nil || got.OwnerID != 0 {
		t.Errorf("a set of the catalog: %+v", got)
	}
	// A second import replaces the catalog's sets and keeps the user's.
	if err := cat.Replace(ctx, &catalog.Catalog{
		Answers: map[[2]string]catalog.Answer{{"messages.getAvailableReactions", ""}: {Hash: 2, Data: []byte("r")}},
		Now:     400,
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ = cat.SetByID(ctx, 20); got != nil {
		t.Errorf("a set the catalog no longer has stays: %+v", got)
	}
	if got, _ = cat.SetByID(ctx, 500); got == nil {
		t.Errorf("the user's set is gone with the catalog's")
	}
	versions, err := cat.SetVersions(ctx)
	if err != nil || len(versions) != 1 || versions[0].ID != 500 || versions[0].Hash != changed.Hash {
		t.Errorf("SetVersions: %v, %v", versions, err)
	}

	// The account's sets, the last made first; deleting is the owner's.
	if _, err := s.Create(ctx, 7, New(700, 701, "Dogs", "MyDogs", installed.Stickers, false), 0, nil, 500); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.Owned(ctx, 7); err != nil || !slices.Equal(ids, []int64{700, 500}) {
		t.Errorf("Owned: %v, %v", ids, err)
	}
	if ids, _ := s.Owned(ctx, 8); len(ids) != 0 {
		t.Errorf("the sets of an account that made none: %v", ids)
	}
	if err := s.Delete(ctx, 500, 8); !errors.Is(err, ErrNoSet) {
		t.Errorf("a deletion by another account: %v", err)
	}
	if err := s.Delete(ctx, 500, 7); err != nil {
		t.Fatal(err)
	}
	if got, _ = cat.SetByID(ctx, 500); got != nil {
		t.Errorf("the deleted set: %+v", got)
	}
	if id, _ := s.SetOf(ctx, 1002); id != 0 {
		t.Errorf("a sticker of a deleted set is in set %d", id)
	}
	if ids, _ := cat.CatalogDocuments(ctx, []int64{1002}); !slices.Equal(ids, []int64{1002}) {
		t.Errorf("a sticker of a deleted set is no longer fetched: %v", ids)
	}
	if taken, _ := s.ShortNameTaken(ctx, "mycats"); taken {
		t.Errorf("the short name of a deleted set is taken")
	}
	if err := s.Delete(ctx, 500, 7); !errors.Is(err, ErrNoSet) {
		t.Errorf("a second deletion: %v", err)
	}
}
