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

package pushdevices

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The table of migrate-20261025-push-devices.sql on the production MySQL;
// the unit tests run the same rules on MemStore.

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
	return time.Now().UnixMilli()%1_000_000_000*1000 + itUserSeq.Add(10)
}

func itDB() *sqlx.DB {
	c := itMySQL.Config()
	return sqlx.NewMySQL(&c)
}

func itStore(t *testing.T) *MySQLStore {
	t.Helper()
	sealer, err := NewSealer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return NewMySQLStore(itDB(), sealer)
}

func TestMySQLStore(t *testing.T) {
	testStore(t, func() Store { return itStore(t) }, itUser)
}

// The secret is sealed in the row, and a row that does not open fails the
// listing instead of sending pushes with a wrong key.
func TestMySQLStoreSealsSecrets(t *testing.T) {
	ctx := context.Background()
	s, user := itStore(t), itUser()
	d := &Device{UserId: user, AuthKeyId: user*100 + 81, TokenType: TokenFCM, Token: "t", Secret: testSecret(0x77)}
	if err := s.Register(ctx, d); err != nil {
		t.Fatal(err)
	}
	var row struct {
		Secret []byte `db:"secret"`
	}
	if err := itDB().QueryRowPartial(ctx, &row, "select secret from push_devices where id = ?", d.Id); err != nil {
		t.Fatal(err)
	}
	if len(row.Secret) == 0 || bytes.Contains(row.Secret, d.Secret[:16]) {
		t.Fatalf("the stored secret is not sealed: %d bytes", len(row.Secret))
	}
	if _, err := itDB().Exec(ctx, "update push_devices set auth_key_id = ? where id = ?", user*100+82, d.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, user); !errors.Is(err, ErrSealedSecretInvalid) {
		t.Fatalf("List of a moved row: %v, want ErrSealedSecretInvalid", err)
	}
	// Removing needs no sealer.
	if n, err := NewMySQLStore(itDB(), nil).DeleteAuthorizations(ctx, user, user*100+82); err != nil || n != 1 {
		t.Fatalf("DeleteAuthorizations without a sealer: %d %v", n, err)
	}
}
