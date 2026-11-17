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

package inline

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The popular GIFs on the production schema: a GIF saved by three people
// or more scores the number of them, one saved by fewer scores nothing for
// it; sends from @gif's results of the last 30 UTC days add up and count
// on their own; older sends neither count nor stay.
func TestGifCountsOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	if err = mysql.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	conf := mysql.Config()
	db := sqlx.NewMySQL(&conf)

	saved := map[int64][]int64{ // document: the people who saved it
		101: {1, 2, 3, 4}, // 4
		102: {1, 2},       // below the threshold
		103: {5, 6},       // below the threshold, but sent
		104: {1, 2, 3},    // 3
	}
	order := int64(1)
	for doc, users := range saved {
		for _, u := range users {
			if _, err = db.Exec(ctx, "INSERT INTO user_saved_gifs (user_id, document_id, order2) VALUES (?, ?, ?)", u, doc, order); err != nil {
				t.Fatal(err)
			}
			order++
		}
	}
	now := time.Date(2026, 11, 30, 1, 0, 0, 0, time.FixedZone("UTC+3", 3*3600)) // 2026-11-29 22:00 UTC
	sends := []struct {
		doc   int64
		day   string
		sends int
	}{
		{103, "2026-11-29", 2},
		{103, "2026-10-31", 3}, // the first of the 30 days: 103 scores 5
		{105, "2026-11-20", 1}, // 1, never saved
		{106, "2026-10-30", 9}, // a day too old
		{101, "2026-09-01", 50},
	}
	for _, s := range sends {
		if _, err = db.Exec(ctx, "INSERT INTO inline_gif_sends (document_id, day, sends) VALUES (?, ?, ?)", s.doc, s.day, s.sends); err != nil {
			t.Fatal(err)
		}
	}

	counts := NewGifCounts(db)
	got, err := counts.PopularGifs(ctx, now, localPopularMax)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{103, 101, 104, 105}; !slices.Equal(got, want) {
		t.Fatalf("popular %v, want %v", got, want)
	}
	var old []int64
	if err = db.QueryRowsPartial(ctx, &old, "SELECT document_id FROM inline_gif_sends WHERE day < '2026-10-31' ORDER BY document_id"); err != nil {
		t.Fatal(err)
	}
	if len(old) != 0 {
		t.Fatalf("old sends left: %v", old)
	}
	var kept []int64
	if err = db.QueryRowsPartial(ctx, &kept, "SELECT document_id FROM inline_gif_sends ORDER BY document_id, day"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kept, []int64{103, 103, 105}) {
		t.Fatalf("sends kept %v", kept)
	}

	if got, err = counts.PopularGifs(ctx, now, 2); err != nil || !slices.Equal(got, []int64{103, 101}) {
		t.Fatalf("limit 2: %v, %v", got, err)
	}
	// Equal scores: the newest document first.
	if _, err = db.Exec(ctx, "INSERT INTO inline_gif_sends (document_id, day, sends) VALUES (107, '2026-11-29', 4)"); err != nil {
		t.Fatal(err)
	}
	if got, err = counts.PopularGifs(ctx, now, localPopularMax); err != nil || !slices.Equal(got, []int64{103, 107, 101, 104, 105}) {
		t.Fatalf("with 107: %v, %v", got, err)
	}

	if _, err = db.Exec(ctx, "DELETE FROM inline_gif_sends"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "DELETE FROM user_saved_gifs"); err != nil {
		t.Fatal(err)
	}
	if got, err = counts.PopularGifs(ctx, now, localPopularMax); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nothing popular: %#v, %v", got, err)
	}
}
