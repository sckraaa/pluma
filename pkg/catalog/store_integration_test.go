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

package catalog

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// A catalog replaces the stored one whole; sets are found by id, short
// name (in any case) and kind; the documents a catalog became stay across
// catalogs (migrate-20261102-catalog.sql).
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	s := NewStore(sqlx.NewMySQL(&conf))

	if a, err := s.Answer(ctx, "messages.getAvailableReactions", ""); err != nil || a != nil {
		t.Fatalf("an empty catalog: %v, %v", a, err)
	}
	if err := s.Replace(ctx, &Catalog{}); !errors.Is(err, ErrEmptyCatalog) {
		t.Fatalf("an empty replacement: %v", err)
	}
	first := &Catalog{
		Answers: map[[2]string]Answer{
			{"messages.getAvailableReactions", ""}: {Hash: 7, Data: []byte("reactions")},
			{"messages.getEmojiKeywords", "ru"}:    {Hash: 3, Data: []byte("слова")},
		},
		Sets: []*Set{
			{ID: 10, AccessHash: 11, ShortName: "AnimatedEmojies", Kind: KindAnimatedEmoji, Hash: 5, Data: []byte("set10")},
			{ID: 20, AccessHash: 21, ShortName: "HotCherry", Data: []byte("set20")},
		},
		Now: 100,
	}
	if err := s.Replace(ctx, first); err != nil {
		t.Fatal(err)
	}
	if a, err := s.Answer(ctx, "messages.getEmojiKeywords", "ru"); err != nil || a == nil || a.Hash != 3 || string(a.Data) != "слова" {
		t.Fatalf("an answer: %+v, %v", a, err)
	}
	if set, err := s.SetByKind(ctx, KindAnimatedEmoji); err != nil || set == nil || set.ID != 10 || set.Hash != 5 {
		t.Fatalf("by kind: %+v, %v", set, err)
	}
	if set, err := s.SetByShortName(ctx, "hotcherry"); err != nil || set == nil || set.ID != 20 || set.Kind != "" {
		t.Fatalf("by short name: %+v, %v", set, err)
	}
	if set, err := s.SetByID(ctx, 20); err != nil || set == nil || set.AccessHash != 21 || string(set.Data) != "set20" {
		t.Fatalf("by id: %+v, %v", set, err)
	}

	// A second catalog without HotCherry and the keywords.
	second := &Catalog{
		Answers: map[[2]string]Answer{{"messages.getAvailableReactions", ""}: {Hash: 8, Data: []byte("new")}},
		Sets:    []*Set{{ID: 10, AccessHash: 11, ShortName: "AnimatedEmojies", Kind: KindAnimatedEmoji, Hash: 6, Data: []byte("set10b")}},
		Now:     200,
	}
	if err := s.Replace(ctx, second); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Answer(ctx, "messages.getEmojiKeywords", "ru"); a != nil {
		t.Fatal("an answer of the old catalog stayed")
	}
	if set, _ := s.SetByID(ctx, 20); set != nil {
		t.Fatal("a set of the old catalog stayed")
	}
	if set, _ := s.SetByKind(ctx, KindAnimatedEmoji); set == nil || set.Hash != 6 {
		t.Fatalf("the new set: %+v", set)
	}

	// A catalog that cannot be stored leaves the stored one.
	bad := &Catalog{
		Answers: map[[2]string]Answer{
			{"help.getTimezonesList", ""}:                           {Data: []byte("tz")},
			{"messages.getEmojiKeywords", strings.Repeat("x", 200)}: {Data: []byte("too long an argument")},
		},
	}
	if err := s.Replace(ctx, bad); err == nil {
		t.Fatal("an answer that does not fit stored")
	}
	if a, _ := s.Answer(ctx, "messages.getAvailableReactions", ""); a == nil || a.Hash != 8 {
		t.Fatalf("after a failed replacement: %+v", a)
	}

	key := sha256.Sum256([]byte("file and set"))
	if id, err := s.DocumentID(ctx, key); err != nil || id != 0 {
		t.Fatalf("an unknown document: %d, %v", id, err)
	}
	if err := s.PutDocument(ctx, key, 99); err != nil {
		t.Fatal(err)
	}
	if id, err := s.DocumentID(ctx, key); err != nil || id != 99 {
		t.Fatalf("a document: %d, %v", id, err)
	}
	if ids, err := s.CatalogDocuments(ctx, []int64{5, 99, 7, 99}); err != nil || len(ids) != 1 || ids[0] != 99 {
		t.Fatalf("documents of the catalog: %v, %v", ids, err)
	}

	// An id given once stays, whatever a later call would give.
	next := int64(500)
	newID := func() (int64, int64, error) { next++; return next, -next, nil }
	id1, h1, err := s.IDFor(ctx, "set", "hotcherry", newID)
	if err != nil || id1 != 501 || h1 != -501 {
		t.Fatalf("a new id: %d %d %v", id1, h1, err)
	}
	if id, h, err := s.IDFor(ctx, "set", "hotcherry", newID); err != nil || id != id1 || h != h1 {
		t.Fatalf("again: %d %d %v", id, h, err)
	}
	if id, _, err := s.IDFor(ctx, "theme", "hotcherry", newID); err != nil || id == id1 {
		t.Fatalf("another kind: %d %v", id, err)
	}
	// Kinds and arguments compare byte by byte: the dice are six kinds.
	dice := &Catalog{Answers: map[[2]string]Answer{{"account.getWallPaper", "slug:Ab"}: {Data: []byte("a")}, {"account.getWallPaper", "slug:ab"}: {Data: []byte("b")}}}
	for i, e := range []string{"🎲", "🎯", "🏀", "⚽", "🎳", "🎰"} {
		dice.Sets = append(dice.Sets, &Set{ID: int64(40 + i), ShortName: fmt.Sprintf("Dice%d", i), Kind: "inputStickerSetDice:" + e, Data: []byte(e)})
	}
	if err := s.Replace(ctx, dice); err != nil {
		t.Fatalf("six dice and two slugs by case: %v", err)
	}
	if set, err := s.SetByKind(ctx, "inputStickerSetDice:🏀"); err != nil || set == nil || string(set.Data) != "🏀" {
		t.Fatalf("a die: %+v, %v", set, err)
	}

	// A set fetched on demand stays through a new catalog; one the catalog
	// then has becomes the catalog's.
	if err := s.PutSet(ctx, &Set{ID: 70, AccessHash: 71, ShortName: "Free420", Hash: 3, Data: []byte("demand")}, 300); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSet(ctx, &Set{ID: 72, AccessHash: 73, ShortName: "roflmoji", Data: []byte("demand")}, 300); err != nil {
		t.Fatal(err)
	}
	// An account installed one die: it stays without its kind.
	if _, err := s.db.Exec(ctx, "INSERT INTO user_sticker_sets (user_id, set_id, kind, order2, installed_at) VALUES (5, 41, 0, 1, 300)"); err != nil {
		t.Fatal(err)
	}
	again := &Catalog{
		Answers: map[[2]string]Answer{{"help.getTimezonesList", ""}: {Data: []byte("tz")}},
		Sets:    []*Set{{ID: 72, AccessHash: 73, ShortName: "roflmoji", Data: []byte("catalog")}},
	}
	if err := s.Replace(ctx, again); err != nil {
		t.Fatal(err)
	}
	if set, err := s.SetByShortName(ctx, "free420"); err != nil || set == nil || string(set.Data) != "demand" {
		t.Fatalf("a set on demand after a new catalog: %+v, %v", set, err)
	}
	if set, err := s.SetByShortName(ctx, "roflmoji"); err != nil || set == nil || string(set.Data) != "catalog" {
		t.Fatalf("a set the catalog took over: %+v, %v", set, err)
	}
	if set, _ := s.SetByKind(ctx, "inputStickerSetDice:🏀"); set != nil {
		t.Fatal("a set of the old catalog stayed")
	}
	if set, err := s.SetByID(ctx, 41); err != nil || set == nil || set.Kind != "" || string(set.Data) != "🎯" {
		t.Fatalf("an installed set of the old catalog: %+v, %v", set, err)
	}
	if set, _ := s.SetByKind(ctx, "inputStickerSetDice:🎯"); set != nil {
		t.Fatal("an installed set of the old catalog kept its kind")
	}
}
