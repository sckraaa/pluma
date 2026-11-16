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
	"fmt"
	"os"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The tables of migrate-20261120-cdn.sql on the production MySQL; the unit
// tests run the same suite on the MemStore.

const migration = "migrate-20261120-cdn.sql"

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

// TestMigrationTwice applies the migration again, twice, over the schema
// that already has it and data in its tables: nothing changes.
func TestMigrationTwice(t *testing.T) {
	ctx := context.Background()
	s := itStore()
	f := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	sec := mustSecrets(t)
	if _, err := s.SetKey(ctx, f.RowId, sec); err != nil {
		t.Fatal(err)
	}
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	for run := range 2 {
		if err := itMySQL.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		got, err := s.ByToken(ctx, sec.Token)
		if err != nil || got == nil || got.RowId != f.RowId {
			t.Fatalf("run %d: the file is gone: %v, %v", run, got, err)
		}
	}
}

// TestMigrationOnProductionSchema applies the migration to the schema
// production has before it, as the migrate service does, then again.
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
	defer old.Terminate(ctx)
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	for run := range 2 {
		if err := old.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	c := old.Config()
	s := NewMySQLStore(sqlx.NewMySQL(&c))
	f := ensure(t, s, FileKey{Kind: KindPhoto, Id: suiteId(), Thumb: "y"}, 2<<20)
	if err := s.Enqueue(ctx, f.RowId, 201, 0, 1); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RotateRtKey(ctx, 201, make([]byte, RtKeySize), 1, 1); err != nil || n == nil {
		t.Fatalf("RotateRtKey: %v, %v", n, err)
	}
}
