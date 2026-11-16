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

package business

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The Business profile on the production MySQL with the whole schema
// (migrate-20261119-business.sql): what is set reads back, the three parts
// are independent, the row goes when nothing is left, two writes at once
// both stay, and the migration run again keeps the data.
func TestProfileStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	s := NewStore(conn)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tx := func(f func(tx *sqlx.Tx) error) {
		t.Helper()
		must(conn.Transact(ctx, f))
	}
	profile := func(user int64) Profile {
		t.Helper()
		p, err := s.Profile(ctx, user)
		must(err)
		return p
	}
	rows := func(user int64) int {
		t.Helper()
		var n []struct {
			N int `db:"n"`
		}
		must(conn.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) AS n FROM business_profiles WHERE user_id = ?", user))
		return n[0].N
	}

	if p := profile(1); p.Hours != nil || p.Location != nil || p.Intro != nil {
		t.Fatalf("a profile nobody set: %+v", p)
	}

	hours := &Hours{TimezoneID: "Europe/Berlin", Intervals: iv(0, 120, 540, 1080, sunday+1320, week)}
	tx(func(tx *sqlx.Tx) error { return SetHoursTx(tx, 1, hours, 100) })
	tx(func(tx *sqlx.Tx) error {
		return SetLocationTx(tx, 1, &Location{Address: "Unter den Linden 1, Берлин", HasGeo: true, Lat: 52.5170365, Long: 13.3888599}, 100)
	})
	tx(func(tx *sqlx.Tx) error {
		return SetIntroTx(tx, 1, &Intro{Title: "Привет \U0001F600", Description: "Ask", StickerID: 777}, 100)
	})
	tx(func(tx *sqlx.Tx) error { return SetLocationTx(tx, 2, &Location{Address: "No point"}, 100) })

	p := profile(1)
	if p.Hours == nil || p.Hours.TimezoneID != "Europe/Berlin" || !slices.Equal(p.Hours.Intervals, hours.Intervals) {
		t.Errorf("the hours read as %+v", p.Hours)
	}
	if l := p.Location; l == nil || l.Address != "Unter den Linden 1, Берлин" || !l.HasGeo || l.Lat != 52.5170365 || l.Long != 13.3888599 {
		t.Errorf("the location read as %+v", l)
	}
	if i := p.Intro; i == nil || i.Title != "Привет \U0001F600" || i.Description != "Ask" || i.StickerID != 777 {
		t.Errorf("the intro read as %+v", i)
	}
	if p2 := profile(2); p2.Location == nil || p2.Location.Address != "No point" || p2.Location.HasGeo || p2.Hours != nil || p2.Intro != nil {
		t.Errorf("the other account: %+v", p2)
	}

	// The location without its point drops the point; removing the hours
	// keeps the rest.
	tx(func(tx *sqlx.Tx) error {
		return SetLocationTx(tx, 1, &Location{Address: "Elsewhere", Lat: 1, Long: 2}, 200)
	})
	tx(func(tx *sqlx.Tx) error { return SetHoursTx(tx, 1, nil, 200) })
	p = profile(1)
	if p.Hours != nil || p.Location == nil || p.Location.Address != "Elsewhere" || p.Location.HasGeo || p.Location.Lat != 0 || p.Intro == nil {
		t.Errorf("after the changes: %+v %+v", p, p.Location)
	}

	// Nothing left: no row.
	tx(func(tx *sqlx.Tx) error { return SetLocationTx(tx, 1, nil, 300) })
	tx(func(tx *sqlx.Tx) error { return SetIntroTx(tx, 1, nil, 300) })
	if p = profile(1); p.Hours != nil || p.Location != nil || p.Intro != nil || rows(1) != 0 {
		t.Errorf("all removed: %+v, %d rows", p, rows(1))
	}
	tx(func(tx *sqlx.Tx) error { return SetIntroTx(tx, 1, nil, 300) }) // nothing to remove

	// Two parts written at once, each in a box transaction of the account
	// as the methods write them: both stay.
	boxDB, err := userbox.Open(db.DSN())
	must(err)
	t.Cleanup(func() { _ = boxDB.Close() })
	for round := range 20 {
		user := int64(100 + round)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		write := func(f func(tx *sqlx.Tx) error) {
			defer wg.Done()
			_, err := userbox.Run(ctx, boxDB, user, func(b *userbox.Box) error { return f(b.Tx()) })
			errs <- err
		}
		wg.Add(2)
		go write(func(tx *sqlx.Tx) error { return SetHoursTx(tx, user, hours, 400) })
		go write(func(tx *sqlx.Tx) error { return SetIntroTx(tx, user, &Intro{Title: "Hi"}, 400) })
		wg.Wait()
		close(errs)
		for err := range errs {
			must(err)
		}
		if p := profile(user); p.Hours == nil || p.Intro == nil {
			t.Errorf("round %d: written at once, read %+v", round, p)
		}
	}

	// The migration again, twice: the tables and the data stay.
	path, err := testinfra.SchemaFile("migrate-20261119-business.sql")
	must(err)
	for range 2 {
		if err = db.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("the migration again: %v", err)
		}
	}
	if p := profile(2); p.Location == nil || p.Location.Address != "No point" {
		t.Errorf("after the migration again: %+v", p)
	}
	for _, table := range []string{"business_auto_reply_settings", "business_auto_replies", "business_auto_reply_log",
		"business_chat_links", "business_bot_connections", "business_bot_paused_peers"} {
		var n []struct {
			N int `db:"n"`
		}
		must(conn.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) AS n FROM "+table))
	}
}
