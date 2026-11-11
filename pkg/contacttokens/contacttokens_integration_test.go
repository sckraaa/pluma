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

package contacttokens

import (
	"context"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The temporary links to a profile (migrate-20261114-user-look.sql): a
// token gives its user until it expires and nobody after; tokens differ;
// expired rows are removed and an account keeps at most liveMax valid ones.
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	s := NewStore(conn)

	const now = int64(1_000_000)
	who := func(token string, at int64) int64 {
		t.Helper()
		id, err := s.Import(ctx, token, at)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	rows := func() int {
		t.Helper()
		var n []int64
		if err := conn.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) FROM contact_tokens"); err != nil {
			t.Fatal(err)
		}
		return int(n[0])
	}

	a, expires, err := s.Export(ctx, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if !Valid(a) || expires != now+Lifetime {
		t.Fatalf("token %q until %d, want a valid one until %d", a, expires, now+Lifetime)
	}
	b, _, err := s.Export(ctx, 8, now+10)
	if err != nil || a == b {
		t.Fatalf("a second token %q (%v), the first %q", b, err, a)
	}
	if who(a, now) != 7 || who(a, expires-1) != 7 || who(b, now+10) != 8 {
		t.Error("a valid token did not give its user")
	}
	if who(a, expires) != 0 || who(a, expires+100) != 0 {
		t.Error("an expired token gave a user")
	}
	if who("abcdefghijklmnopqrstuvwx", now) != 0 || who("", now) != 0 || who(a+"x", now) != 0 {
		t.Error("an unknown token gave a user")
	}

	// A new token of the account does not end the one already shown.
	a2, _, err := s.Export(ctx, 7, now+20)
	if err != nil {
		t.Fatal(err)
	}
	if who(a, now+20) != 7 || who(a2, now+20) != 7 {
		t.Error("the tokens of one account are not valid together")
	}

	// The expired rows of everybody go with the next export.
	if _, _, err = s.Export(ctx, 9, expires+Lifetime); err != nil {
		t.Fatal(err)
	}
	if n := rows(); n != 1 {
		t.Errorf("%d rows after everything else expired, want 1", n)
	}

	// An account keeps liveMax valid tokens: the oldest go.
	var first string
	for i := int64(0); i < liveMax+3; i++ {
		tok, _, err := s.Export(ctx, 10, 5_000_000+i)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = tok
		}
	}
	var live []int64
	if err = conn.QueryRowsPartial(ctx, &live, "SELECT COUNT(*) FROM contact_tokens WHERE user_id = 10"); err != nil {
		t.Fatal(err)
	}
	if live[0] != liveMax || who(first, 5_000_010) != 0 {
		t.Errorf("%d tokens of one account, the oldest gives %d; want %d and nobody", live[0], who(first, 5_000_010), liveMax)
	}
}
