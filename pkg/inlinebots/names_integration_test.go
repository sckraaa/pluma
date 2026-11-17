//go:build integration

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

package inlinebots_test

import (
	"context"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// z_pluma-inline-names.sql names the bots as the registry does: on a new
// database, where it runs after the accounts are made, and on one that has
// the names of z_pluma-inline-bots.sql, as production does. It runs again
// without change and makes no account that is absent.
func TestInlineBotNamesOnMySQL(t *testing.T) {
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
	file, err := testinfra.SchemaFile("z_pluma-inline-names.sql")
	if err != nil {
		t.Fatal(err)
	}
	type nameRow struct {
		Id        int64  `db:"id"`
		FirstName string `db:"first_name"`
		LastName  string `db:"last_name"`
	}
	names := func() map[int64]nameRow {
		t.Helper()
		var rows []nameRow
		if err := db.QueryRowsPartial(ctx, &rows,
			"SELECT id, first_name, last_name FROM users WHERE id IN (?, ?)", inlinebots.Gif.Id, inlinebots.Pic.Id); err != nil {
			t.Fatal(err)
		}
		out := make(map[int64]nameRow, len(rows))
		for _, r := range rows {
			out[r.Id] = r
		}
		return out
	}
	named := func(when string) {
		t.Helper()
		got := names()
		for _, b := range inlinebots.All() {
			if r, ok := got[b.Id]; !ok || r.FirstName != b.Name || r.LastName != "" {
				t.Errorf("%s: @%s is %+v (%v), want %q", when, b.Username, r, ok, b.Name)
			}
		}
	}

	// A new database: the schema made the accounts and renamed them.
	named("after the schema")

	// The names production has from z_pluma-inline-bots.sql.
	if _, err = db.Exec(ctx, "UPDATE users SET first_name = 'GIF' WHERE id = ?", inlinebots.Gif.Id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE users SET first_name = 'Pic' WHERE id = ?", inlinebots.Pic.Id); err != nil {
		t.Fatal(err)
	}
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	named("over the old names")
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	named("after a second run")

	// An account that was never made (its username was taken) stays
	// absent; the other is renamed.
	if _, err = db.Exec(ctx, "DELETE FROM users WHERE id = ?", inlinebots.Pic.Id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE users SET first_name = 'GIF' WHERE id = ?", inlinebots.Gif.Id); err != nil {
		t.Fatal(err)
	}
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	got := names()
	if _, ok := got[inlinebots.Pic.Id]; ok {
		t.Errorf("an account of @pic made: %+v", got[inlinebots.Pic.Id])
	}
	if r := got[inlinebots.Gif.Id]; r.FirstName != inlinebots.Gif.Name {
		t.Errorf("@gif is %+v", r)
	}
}
