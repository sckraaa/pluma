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

package migrate_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/migrate"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	_ "github.com/go-sql-driver/mysql"
)

// TestUpOnRealMySQL applies the real schema files to an empty database with
// the service user, as the migrate service does, and checks the history.
func TestUpOnRealMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	m, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles([]string{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Container.Terminate(context.Background()) })

	dsn, err := migrate.DSN(m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	files, err := migrate.Load("../../teamgramd/deploy/sql")
	if err != nil {
		t.Fatal(err)
	}
	var ran []string
	if err := migrate.Up(ctx, db, files, func(name string, _ time.Duration) { ran = append(ran, name) }); err != nil {
		t.Fatal(err)
	}
	if len(ran) != len(files) {
		t.Fatalf("applied %d of %d files", len(ran), len(files))
	}
	var users int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = 777000").Scan(&users); err != nil || users != 1 {
		t.Fatalf("z_init.sql did not run: %d %v", users, err)
	}

	// Again: nothing to do.
	ran = nil
	if err := migrate.Up(ctx, db, files, func(name string, _ time.Duration) { ran = append(ran, name) }); err != nil || len(ran) != 0 {
		t.Fatalf("second run applied %v, %v", ran, err)
	}

	// An applied file that changed is refused.
	changed := append([]migrate.File(nil), files...)
	changed[3] = migrate.NewFile(changed[3].Name, changed[3].SQL+"\n-- edited")
	if err := migrate.Up(ctx, db, changed, nil); err == nil {
		t.Fatal("edited applied file accepted")
	}

	// A new file is applied on top.
	extra := migrate.NewFile("zz_test.sql", "CREATE TABLE migrate_probe (id INT PRIMARY KEY); INSERT INTO migrate_probe VALUES (1);")
	if err := migrate.Up(ctx, db, append(files, extra), nil); err != nil {
		t.Fatal(err)
	}
	// A failing file is reported and not recorded.
	bad := migrate.NewFile("zz_zbad.sql", "INSERT INTO migrate_probe VALUES (2); INSERT INTO no_such_table VALUES (1);")
	if err := migrate.Up(ctx, db, append(files, extra, bad), nil); err == nil {
		t.Fatal("failing file accepted")
	}
	applied, err := migrate.Applied(ctx, db)
	if err != nil || applied["zz_zbad.sql"] != "" || applied["zz_test.sql"] == "" {
		t.Fatalf("history after a failed file: %v %v", applied, err)
	}
}

// TestBaselineOfAnOldDatabase: a database built before versioned migrations
// is refused by Up until baseline records what it has.
func TestBaselineOfAnOldDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	m, err := testinfra.StartMySQL(ctx) // the whole schema, as the init directory built it
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Container.Terminate(context.Background()) })
	dsn, _ := migrate.DSN(m.DSN())
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := migrate.Load("../../teamgramd/deploy/sql")
	if err != nil {
		t.Fatal(err)
	}

	if err := migrate.Up(ctx, db, files, nil); !errors.Is(err, migrate.ErrNoHistory) {
		t.Fatalf("Up on an old database: %v, want ErrNoHistory", err)
	}
	n, err := migrate.Baseline(ctx, db, files, files[len(files)-1].Name)
	if err != nil || n != len(files) {
		t.Fatalf("baseline recorded %d, %v", n, err)
	}
	if err := migrate.Up(ctx, db, files, nil); err != nil {
		t.Fatalf("Up after baseline: %v", err)
	}
	if _, err := migrate.Baseline(ctx, db, files, files[0].Name); err == nil {
		t.Fatal("second baseline accepted")
	}
}
