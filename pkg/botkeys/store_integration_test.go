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

package botkeys_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// secretKeysTable is bot_secret_keys of migrate-20261121-bot-platform.sql,
// created here too so that the test also runs on a schema without that
// migration; on the full schema it is a no-op.
const secretKeysTable = `CREATE TABLE IF NOT EXISTS bot_secret_keys (
  bot_id bigint(20) NOT NULL PRIMARY KEY,
  token_hash char(64) NOT NULL,
  webapp_key varbinary(96) NOT NULL,
  login_key varbinary(96) NOT NULL,
  updated_at int(11) NOT NULL
)`

func newSealer(t *testing.T) *botkeys.Sealer {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	s, err := botkeys.NewSealer(hex.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	db := sqlx.NewMySQL(&conf)
	if _, err = db.Exec(ctx, secretKeysTable); err != nil {
		t.Fatal(err)
	}
	sealer := newSealer(t)
	keys := botkeys.NewStore(db, sealer)
	bots := botstore.New(db).WithSecrets(sealer)

	want := func(token string) {
		t.Helper()
		got, ok, err := keys.Get(ctx, mustId(t, token))
		if err != nil || !ok {
			t.Fatalf("get: %v, %v", ok, err)
		}
		d := botkeys.Derive(token)
		if !bytes.Equal(got.WebApp, d.WebApp) || !bytes.Equal(got.Login, d.Login) {
			t.Fatal("the stored keys are not those of the token")
		}
	}

	// Create stores the keys of the first token.
	id, first, err := bots.Create(ctx, "keys_bot", "Keys", 0)
	if err != nil {
		t.Fatal(err)
	}
	want(first)

	// A new token replaces them; the old token's keys are never given out
	// again, and a late login with the old token does not bring them back.
	second, err := bots.NewToken(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want(second)
	_, firstHash, _ := botstore.ParseToken(first)
	if wrote, err := keys.Ensure(ctx, id, first, firstHash); err != nil || wrote {
		t.Fatalf("ensure with the old token: %v, %v", wrote, err)
	}
	want(second)

	// A login with the current token whose keys are stored writes nothing.
	_, secondHash, _ := botstore.ParseToken(second)
	if wrote, err := keys.Ensure(ctx, id, second, secondHash); err != nil || wrote {
		t.Fatalf("ensure again: %v, %v", wrote, err)
	}

	// A token issued without an encryption key leaves the old keys behind,
	// and they are not read: the bot has no keys until it logs in.
	third, err := botstore.New(db).NewToken(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := keys.Get(ctx, id); ok || err != nil {
		t.Fatalf("keys of a replaced token: %v, %v", ok, err)
	}
	_, thirdHash, _ := botstore.ParseToken(third)
	if wrote, err := keys.Ensure(ctx, id, third, thirdHash); err != nil || !wrote {
		t.Fatalf("login after the token: %v, %v", wrote, err)
	}
	want(third)

	// Another encryption key (the old one lost): the keys do not open, and
	// the next login seals them again under the new one.
	lost := botkeys.NewStore(db, newSealer(t))
	if _, ok, err := lost.Get(ctx, id); ok || !errors.Is(err, botkeys.ErrSealedInvalid) {
		t.Fatalf("another encryption key: %v, %v", ok, err)
	}
	if wrote, err := lost.Ensure(ctx, id, third, thirdHash); err != nil || !wrote {
		t.Fatalf("login under the new key: %v, %v", wrote, err)
	}
	if _, ok, err := lost.Get(ctx, id); !ok || err != nil {
		t.Fatalf("after the login: %v, %v", ok, err)
	}

	// A bot without a row.
	other, _, err := botstore.New(db).Create(ctx, "nokeys_bot", "No keys", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := keys.Get(ctx, other); ok || err != nil {
		t.Fatalf("a bot without keys: %v, %v", ok, err)
	}
}

func mustId(t *testing.T, token string) int64 {
	t.Helper()
	id, _, ok := botstore.ParseToken(token)
	if !ok {
		t.Fatalf("not a token")
	}
	return id
}
