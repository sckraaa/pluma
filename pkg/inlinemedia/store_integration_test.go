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

package inlinemedia_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

const migration = "migrate-20261119-inline-bots.sql"

// The tables of migrate-20261119-inline-bots.sql on the production MySQL:
// the file runs again without change, two imports of one key at once keep
// one row with the first medium, a showing moves last_served_at only when
// it is stale, and the sends of a GIF add up per UTC day.
func TestStoreOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	if err = mysql.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	file, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatalf("second run of %s: %v", migration, err)
	}
	conf := mysql.Config()
	db := sqlx.NewMySQL(&conf)
	store := inlinemedia.NewStore(db)

	key := inlinemedia.KeyOf("searxng", "https://pics.example/1.jpg")
	created := time.Now().Add(-2 * time.Hour).Unix()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.Put(ctx, inlinemedia.Row{
				Provider: "searxng", SourceKey: key[:], Kind: inlinemedia.KindPhoto,
				MediaId: int64(100 + i), Bytes: 1000, CreatedAt: created, ServedAt: created,
			})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	var n []int64
	if err = db.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) FROM inline_media"); err != nil || len(n) != 1 || n[0] != 1 {
		t.Fatalf("rows %v, %v; want 1", n, err)
	}
	rows, err := store.Lookup(ctx, "searxng", []inlinemedia.Key{key, inlinemedia.KeyOf("searxng", "other")})
	if err != nil || len(rows) != 1 {
		t.Fatalf("lookup %v, %v", rows, err)
	}
	first := rows[key]
	if first.MediaId < 100 || first.MediaId >= 108 || first.Kind != inlinemedia.KindPhoto || first.Bytes != 1000 {
		t.Fatalf("row %+v", first)
	}
	if other, err := store.Lookup(ctx, "klipy", []inlinemedia.Key{key}); err != nil || len(other) != 0 {
		t.Fatalf("another provider's lookup: %v, %v", other, err)
	}
	// A later import of the key keeps the medium.
	if err = store.Put(ctx, inlinemedia.Row{Provider: "searxng", SourceKey: key[:], Kind: inlinemedia.KindPhoto, MediaId: 999, Bytes: 5, CreatedAt: created, ServedAt: created}); err != nil {
		t.Fatal(err)
	}
	if rows, _ = store.Lookup(ctx, "searxng", []inlinemedia.Key{key}); rows[key].MediaId != first.MediaId {
		t.Fatalf("medium %d replaced by %d", first.MediaId, rows[key].MediaId)
	}

	now := time.Now()
	if err = store.Touch(ctx, "searxng", []inlinemedia.Key{key}, now); err != nil {
		t.Fatal(err)
	}
	if rows, _ = store.Lookup(ctx, "searxng", []inlinemedia.Key{key}); rows[key].ServedAt != now.Unix() {
		t.Fatalf("served at %d, want %d", rows[key].ServedAt, now.Unix())
	}
	if err = store.Touch(ctx, "searxng", []inlinemedia.Key{key}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if rows, _ = store.Lookup(ctx, "searxng", []inlinemedia.Key{key}); rows[key].ServedAt != now.Unix() {
		t.Fatalf("a fresh showing written again: %d", rows[key].ServedAt)
	}

	day := time.Date(2026, 11, 19, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600)) // 2026-11-20 UTC
	for i := 0; i < 3; i++ {
		if err = store.CountGifSend(ctx, 77, day); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.CountGifSend(ctx, 77, day.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	type sendRow struct {
		Day   string `db:"day"`
		Sends int64  `db:"sends"`
	}
	var sends []sendRow
	if err = db.QueryRowsPartial(ctx, &sends, "SELECT DATE_FORMAT(day, '%Y-%m-%d') AS day, sends FROM inline_gif_sends WHERE document_id = 77 ORDER BY day"); err != nil {
		t.Fatal(err)
	}
	if len(sends) != 2 || sends[0] != (sendRow{"2026-11-20", 3}) || sends[1] != (sendRow{"2026-11-21", 1}) {
		t.Fatalf("sends %+v", sends)
	}
}
