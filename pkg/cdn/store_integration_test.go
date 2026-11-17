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

package cdn

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The tables of migrate-20261120-cdn.sql as migrate-20261125-https-cdn.sql
// leaves them, on the production MySQL; the unit tests run the same suite
// on the MemStore.

const (
	migration102 = "migrate-20261120-cdn.sql"
	migration    = "migrate-20261125-https-cdn.sql"
)

var itMySQL *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	if itMySQL != nil {
		_ = itMySQL.Terminate(ctx)
	}
	os.Exit(code)
}

func itStore() *MySQLStore {
	c := itMySQL.Config()
	return NewMySQLStore(sqlx.NewMySQL(&c))
}

func TestMySQLStore(t *testing.T) { runStoreSuite(t, itStore()) }

func applySchema(t *testing.T, m *testinfra.MySQL, names ...string) {
	t.Helper()
	for _, name := range names {
		path, err := testinfra.SchemaFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err = m.ApplySQLFile(context.Background(), path); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func rawDB(t *testing.T, m *testinfra.MySQL) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// autoIncrement drops the counter of SHOW CREATE TABLE, which data moves.
var autoIncrement = regexp.MustCompile(` AUTO_INCREMENT=\d+`)

// schemaOf returns SHOW CREATE TABLE of every CDN table of db, "" for one
// that is missing.
func schemaOf(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range []string{"cdn_files", "cdn_replicas", "cdn_nodes", "cdn_deliveries", "cdn_delivery_usage"} {
		var name, ddl string
		err := db.QueryRow("SHOW CREATE TABLE `"+table+"`").Scan(&name, &ddl)
		if err != nil && strings.Contains(err.Error(), "1146") { // ER_NO_SUCH_TABLE
			out[table] = ""
			continue
		}
		if err != nil {
			t.Fatalf("SHOW CREATE TABLE %s: %v", table, err)
		}
		out[table] = autoIncrement.ReplaceAllString(ddl, "")
	}
	return out
}

// checkSchema checks the tables migration 107 leaves.
func checkSchema(t *testing.T, got map[string]string) {
	t.Helper()
	if got["cdn_nodes"] != "" {
		t.Error("cdn_nodes is still there")
	}
	r := got["cdn_replicas"]
	for _, want := range []string{
		"`target_id` int NOT NULL COMMENT '1: the home origin'",
		"PRIMARY KEY (`file_row_id`,`target_id`)",
		"KEY `k_queue` (`target_id`,`state`,`priority`,`next_try_at`)",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("cdn_replicas has no %s:\n%s", want, r)
		}
	}
	if strings.Contains(r, "`dc_id`") {
		t.Errorf("cdn_replicas keeps dc_id:\n%s", r)
	}
	if !strings.Contains(got["cdn_deliveries"], "PRIMARY KEY (`dc_id`)") {
		t.Errorf("cdn_deliveries:\n%s", got["cdn_deliveries"])
	}
	if !strings.Contains(got["cdn_delivery_usage"], "PRIMARY KEY (`dc_id`,`month`)") {
		t.Errorf("cdn_delivery_usage:\n%s", got["cdn_delivery_usage"])
	}
}

// TestMigrationTwice applies migrations 102 and 107 again, twice, over the
// schema that already has them and data in their tables: nothing changes.
func TestMigrationTwice(t *testing.T) {
	ctx := context.Background()
	s := itStore()
	f := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	sec := mustSecrets(t)
	if _, err := s.SetKey(ctx, f.RowId, sec); err != nil {
		t.Fatal(err)
	}
	target := suiteTarget()
	if err := s.Enqueue(ctx, f.RowId, target, PriorityFirst, 1); err != nil {
		t.Fatal(err)
	}
	dc := suiteDelivery()
	if err := s.AddUsage(ctx, dc, "2026-11", 5, 6); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, itMySQL)
	fresh := schemaOf(t, db)
	checkSchema(t, fresh)
	for run := range 2 {
		applySchema(t, itMySQL, migration102, migration)
		got, err := s.ByToken(ctx, sec.Token)
		if err != nil || got == nil || got.RowId != f.RowId {
			t.Fatalf("run %d: the file is gone: %v, %v", run, got, err)
		}
		if r, err := s.Replica(ctx, f.RowId, target); err != nil || r == nil || r.Priority != PriorityFirst {
			t.Fatalf("run %d: the preparation is gone: %+v, %v", run, r, err)
		}
		if u, err := s.Usage(ctx, dc, "2026-11"); err != nil || u.OriginBytes != 5 || u.EdgeBytesEst != 6 {
			t.Fatalf("run %d: usage %+v, %v", run, u, err)
		}
		if again := schemaOf(t, db); fmt.Sprint(again) != fmt.Sprint(fresh) {
			t.Fatalf("run %d changed the schema:\n%v\nwas\n%v", run, again, fresh)
		}
	}
}

// TestMigrationOnProductionSchema applies migration 107 to the schema
// production has before it, with rows of task 102 in its tables (production
// has none: the worst case), as the migrate service does, then again. The
// tables end as on a database made from all the files.
func TestMigrationOnProductionSchema(t *testing.T) {
	ctx := context.Background()
	before, err := testinfra.SchemaFilesBefore(migration)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(before))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	db := rawDB(t, old)
	if _, err = db.Exec("INSERT INTO cdn_files (id, file_kind, file_id, size, created_at) VALUES (1, 1, 77, 3145728, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO cdn_replicas (file_row_id, dc_id, state, priority, updated_at) VALUES (1, 1, 0, 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO cdn_nodes (dc_id, updated_at) VALUES (201, 1)"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		applySchema(t, old, migration)
	}
	got := schemaOf(t, db)
	checkSchema(t, got)
	if want := schemaOf(t, rawDB(t, itMySQL)); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("migrated schema\n%v\ndiffers from a fresh one\n%v", got, want)
	}

	c := old.Config()
	s := NewMySQLStore(sqlx.NewMySQL(&c))
	// The row of task 102 is the preparation for the home origin now.
	cl, err := s.Claim(ctx, TargetHomeOrigin, time.Minute, 10)
	if err != nil || cl == nil || cl.File.RowId != 1 || cl.Priority != PriorityFirst || cl.File.Key.Id != 77 {
		t.Fatalf("Claim of the old row: %+v, %v", cl, err)
	}
	if ok, err := s.MarkReady(ctx, cl, []byte{1}, 11); !ok || err != nil {
		t.Fatalf("MarkReady: %v, %v", ok, err)
	}
	if err = s.PutDeliveryHealth(ctx, &DeliveryHealth{DcId: 201, HealthyUntil: 5, UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.AddUsage(ctx, 201, "2026-11", 1, 2); err != nil {
		t.Fatal(err)
	}
}

// TestMigrationWithout102 applies migration 107 to a database that never
// had the tables of task 102, twice: it creates its own and leaves the rest.
func TestMigrationWithout102(t *testing.T) {
	ctx := context.Background()
	before, err := testinfra.SchemaFilesBefore(migration102)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(before))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	for range 2 {
		applySchema(t, old, migration)
	}
	got := schemaOf(t, rawDB(t, old))
	if got["cdn_replicas"] != "" || got["cdn_nodes"] != "" || got["cdn_deliveries"] == "" || got["cdn_delivery_usage"] == "" {
		t.Fatalf("tables after 107 alone: %v", got)
	}
}
