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

package webbrowser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The tables of migrate-20261119-web-browser-settings.sql on the production
// MySQL; the unit tests run the same rules on MemStore.

const migration = "migrate-20261119-web-browser-settings.sql"

var itMySQL *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

// itUserSeq makes user ids unique across tests and runs (go test -count=N).
var itUserSeq atomic.Int64

func itUser() int64 {
	return time.Now().UnixMilli()%1_000_000_000*1000 + itUserSeq.Add(1)
}

func itStore() *MySQLStore {
	c := itMySQL.Config()
	return NewMySQLStore(sqlx.NewMySQL(&c))
}

func TestMySQLStore(t *testing.T) {
	testStore(t, func() Store { return itStore() }, itUser)
}

// The migration runs again over its own tables with rows in them: nothing
// changes.
func TestMigrationAgain(t *testing.T) {
	ctx := context.Background()
	s, user := itStore(), itUser()
	if err := s.SetFlags(ctx, user, true, true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutException(ctx, user, Exception{Domain: "a.com", URL: "a.com"}, true); err != nil {
		t.Fatal(err)
	}
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	if err = itMySQL.ApplySQLFile(ctx, path); err != nil {
		t.Fatalf("the migration again: %v", err)
	}
	got, err := s.Get(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OpenExternal || len(got.External) != 1 || got.External[0].Domain != "a.com" {
		t.Fatalf("settings after the migration again: %+v", got)
	}
}

// Two writes of one domain into the two lists at once: it ends in one of
// them, never in both, never lost.
func TestPutExceptionRace(t *testing.T) {
	ctx := context.Background()
	s := itStore()
	for range 20 {
		user := itUser()
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, external := range []bool{true, false} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = s.PutException(ctx, user, Exception{Domain: "race.com", URL: "race.com"}, external)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Get(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.External)+len(got.InApp) != 1 {
			t.Fatalf("race.com in %v and %v, want one list", domains(got.External), domains(got.InApp))
		}
	}
}

// Writes of different domains at once into a list one short of full: one
// gets in, the others get ErrLimit, the list is never longer than the
// limit.
func TestPutExceptionLimitRace(t *testing.T) {
	ctx := context.Background()
	s := itStore()
	for round := range 5 {
		user := itUser()
		for i := range MaxExceptions - 1 {
			if err := s.PutException(ctx, user, Exception{Domain: fmt.Sprintf("d%d.com", i), URL: "x"}, true); err != nil {
				t.Fatal(err)
			}
		}
		const writers = 4
		var (
			wg      sync.WaitGroup
			ok      atomic.Int32
			limited atomic.Int32
		)
		for w := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := s.PutException(ctx, user, Exception{Domain: fmt.Sprintf("new%d-%d.com", round, w), URL: "x"}, true)
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrLimit):
					limited.Add(1)
				default:
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		got, err := s.Get(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.External) != MaxExceptions || ok.Load() != 1 || limited.Load() != writers-1 {
			t.Fatalf("round %d: %d in the list, %d written, %d refused; want %d, 1, %d",
				round, len(got.External), ok.Load(), limited.Load(), MaxExceptions, writers-1)
		}
	}
}

// The domain is compared byte for byte: two hosts the collation of the
// table holds equal are two exceptions.
func TestDomainsByteForByte(t *testing.T) {
	ctx := context.Background()
	s, user := itStore(), itUser()
	for _, d := range []string{"cafe.fr", "café.fr"} {
		if err := s.PutException(ctx, user, Exception{Domain: d, URL: d}, true); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.External) != 2 {
		t.Fatalf("exceptions %v, want two", domains(got.External))
	}
}
