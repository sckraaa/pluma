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
	"errors"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// z_pluma-inline-bots.sql makes the accounts the registry expects, runs
// again without change, and makes nothing of a bot whose username a person
// has taken; the check of bff bots sees each case.
func TestInlineBotAccountsOnMySQL(t *testing.T) {
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
	file, err := testinfra.SchemaFile("z_pluma-inline-bots.sql")
	if err != nil {
		t.Fatal(err)
	}
	count := func(query string, args ...interface{}) int64 {
		t.Helper()
		var n []int64
		if err := db.QueryRowsPartial(ctx, &n, query, args...); err != nil || len(n) != 1 {
			t.Fatalf("%s: %v %v", query, n, err)
		}
		return n[0]
	}
	accounts := func() int64 {
		return count("SELECT COUNT(*) FROM users WHERE id BETWEEN 1000101 AND 1000103") +
			count("SELECT COUNT(*) FROM username WHERE peer_id BETWEEN 1000101 AND 1000103") +
			count("SELECT COUNT(*) FROM bots WHERE bot_id BETWEEN 1000101 AND 1000103")
	}

	// The schema ran the file once; once more changes nothing.
	if got := accounts(); got != 9 {
		t.Fatalf("after the schema: %d rows, want 9", got)
	}
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	if got := accounts(); got != 9 {
		t.Fatalf("after a second run: %d rows, want 9", got)
	}
	health := inlinebots.NewHealth()
	if err = health.Check(ctx, db); err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, b := range inlinebots.All() {
		if !health.Usable(b.Id) {
			t.Errorf("@%s is not served", b.Username)
		}
		var placeholders []string
		if err = db.QueryRowsPartial(ctx, &placeholders, "SELECT bot_inline_placeholder FROM bots WHERE bot_id = ?", b.Id); err != nil || len(placeholders) != 1 || placeholders[0] != inlinebots.Placeholder() {
			t.Errorf("@%s placeholder %q, %v", b.Username, placeholders, err)
		}
	}
	if !health.Verified() {
		t.Error("not verified")
	}

	// The store finds the bots by name, no token signs in as them, and
	// their description is the server's in the viewer's language.
	store := botstore.New(db)
	if id, err := store.ByUsername(ctx, "places"); err != nil || id != inlinebots.Places.Id {
		t.Errorf("places: %d, %v", id, err)
	}
	for _, token := range []string{"1000101:", "1000101:builtin", "builtin:1000101"} {
		if id, err := store.BotIdOfToken(ctx, token); err != nil || id != 0 {
			t.Errorf("token %q: %d, %v", token, id, err)
		}
	}
	info, err := store.BotInfo(ctx, inlinebots.Places.Id, botstore.Viewer{UserId: 9, PeerType: mtproto.PEER_USER, PeerId: 9, Lang: "ru"})
	if err != nil || info.GetDescription_STRING() != langpack.InlineBotText(inlinebots.Places.About, "ru") {
		t.Errorf("bot info: %q, %v", info.GetDescription_STRING(), err)
	}

	// A person has "places" (in other letters: the names compare without
	// case) before the file runs: no account of @places, the others stay.
	if _, err = db.Exec(ctx, "DELETE FROM bots WHERE bot_id = 1000103"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "DELETE FROM username WHERE peer_id = 1000103"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "DELETE FROM users WHERE id = 1000103"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "INSERT INTO username (username, peer_type, peer_id) VALUES ('Places', 2, 136907999)"); err != nil {
		t.Fatal(err)
	}
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	if n := count("SELECT COUNT(*) FROM users WHERE id = 1000103") + count("SELECT COUNT(*) FROM bots WHERE bot_id = 1000103"); n != 0 {
		t.Errorf("@places made over a taken username: %d rows", n)
	}
	if err = health.Check(ctx, db); !errors.Is(err, inlinebots.ErrMisconfigured) {
		t.Fatalf("check over a taken username: %v", err)
	}
	if health.Usable(inlinebots.Places.Id) || !health.Usable(inlinebots.Gif.Id) || !health.Usable(inlinebots.Pic.Id) || health.Verified() {
		t.Errorf("places %v, gif %v, pic %v, verified %v",
			health.Usable(inlinebots.Places.Id), health.Usable(inlinebots.Gif.Id), health.Usable(inlinebots.Pic.Id), health.Verified())
	}

	// The operator frees the name and runs the file: @places is served
	// again at the next check.
	if _, err = db.Exec(ctx, "DELETE FROM username WHERE peer_id = 136907999"); err != nil {
		t.Fatal(err)
	}
	if err = mysql.ApplySQLFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	if err = health.Check(ctx, db); err != nil || !health.Usable(inlinebots.Places.Id) {
		t.Errorf("after the repair: %v, served %v", err, health.Usable(inlinebots.Places.Id))
	}
}
