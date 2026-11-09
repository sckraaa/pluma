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

package moderation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itDB.Terminate(ctx)
	os.Exit(code)
}

func newIntegrationDAO(t *testing.T) (*DAO, *sqlx.DB) {
	t.Helper()
	c := itDB.Config()
	db := sqlx.NewMySQL(&c)
	return New(db), db
}

var seq int64

func nextUser() int64 {
	seq++
	return time.Now().UnixMilli()*1000 + seq
}

func makeReport(t *testing.T, d *DAO, reporter, target int64, ids []int32, snapshot *Snapshot) int64 {
	t.Helper()
	ctx := context.Background()
	if err := d.AddReport(ctx, &Report{ReporterUserId: reporter, Kind: KindMessage, PeerType: 2, PeerId: target, TargetUserId: target, Reason: "spam"}, ids, snapshot); err != nil {
		t.Fatal(err)
	}
	rows, err := d.ReportsOfTarget(ctx, target, 10)
	if err != nil || len(rows) == 0 {
		t.Fatalf("report not stored: %v %v", rows, err)
	}
	return rows[0].Id
}

func TestAddReportDeduplicatesOpen(t *testing.T) {
	d, _ := newIntegrationDAO(t)
	ctx := context.Background()
	reporter, target := nextUser(), nextUser()
	id := makeReport(t, d, reporter, target, []int32{1}, &Snapshot{Messages: []SnapshotMessage{{Id: 1, Text: "first"}}})

	if err := d.AddReport(ctx, &Report{ReporterUserId: reporter, Kind: KindMessage, PeerType: 2, PeerId: target, TargetUserId: target, Reason: "spam", Comment: "added"}, []int32{1},
		&Snapshot{Messages: []SnapshotMessage{{Id: 1, Text: "second"}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := d.ReportsOfTarget(ctx, target, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("a repeated report must stay one row: %d %v", len(rows), err)
	}
	r := rows[0]
	if r.Id != id || r.Comment != "added" {
		t.Errorf("repeat: %+v", r)
	}
	snap, err := r.Snapshot()
	if err != nil || snap.Messages[0].Text != "first" {
		t.Errorf("the snapshot must stay the first: %+v %v", snap, err)
	}

	// After a decision the same report is a new one.
	if err = d.Decide(ctx, Decision{ReportId: id, Actor: "a", Action: ActionDismiss}); err != nil {
		t.Fatal(err)
	}
	if err = d.AddReport(ctx, &Report{ReporterUserId: reporter, Kind: KindMessage, PeerType: 2, PeerId: target, TargetUserId: target, Reason: "spam"}, []int32{1}, &Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if rows, err = d.ReportsOfTarget(ctx, target, 10); err != nil || len(rows) != 2 {
		t.Errorf("a decided report must not block a new one: %d %v", len(rows), err)
	}
}

func TestDecideRestrictInOneTransaction(t *testing.T) {
	d, db := newIntegrationDAO(t)
	ctx := context.Background()
	reporter, target := nextUser(), nextUser()
	id := makeReport(t, d, reporter, target, []int32{1}, &Snapshot{Messages: []SnapshotMessage{{Id: 1}}})

	if err := d.Decide(ctx, Decision{ReportId: id, Actor: "mod", Action: ActionRestrict, Reason: "spam", UntilDate: int32(time.Now().Unix() + 3600)}); err != nil {
		t.Fatal(err)
	}
	r, err := d.Report(ctx, id)
	if err != nil || r.Status != StatusDecided || r.EffectsTotal != 1 || r.EffectsStage != 0 {
		t.Fatalf("report after decision: %+v %v", r, err)
	}
	restr, err := d.Restriction(ctx, target)
	if err != nil || restr == nil || restr.Kind != RestrictLimited {
		t.Fatalf("restriction: %+v %v", restr, err)
	}
	var n int
	if err = db.QueryRow(ctx, &n, "SELECT COUNT(*) FROM admin_audit WHERE action = 'decide_restrict' AND target = ?", fmt.Sprintf("report:%d", id)); err != nil || n != 1 {
		t.Errorf("journal rows: %d %v", n, err)
	}
	// A second decision on the same report changes nothing.
	if err = d.Decide(ctx, Decision{ReportId: id, Actor: "mod", Action: ActionBan}); !errors.Is(err, ErrAlreadyDecided) {
		t.Errorf("repeat decision: %v", err)
	}
	if restr, _ = d.Restriction(ctx, target); restr.Kind != RestrictLimited {
		t.Errorf("a refused decision changed the restriction: %+v", restr)
	}

	// A lift of an expired restriction would be harmless; lifting ends it.
	lifted, err := d.Lift(ctx, target, "appeal", "owner")
	if err != nil || !lifted {
		t.Fatalf("lift: %v %v", lifted, err)
	}
	if restr, err = d.Restriction(ctx, target); err != nil || restr != nil {
		t.Errorf("after lift: %+v %v", restr, err)
	}
}

// A limit never softens a ban that is in force.
func TestRestrictionKeepsStricter(t *testing.T) {
	d, _ := newIntegrationDAO(t)
	ctx := context.Background()
	target := nextUser()
	if changed, err := d.ApplyDirect(ctx, target, RestrictBanned, 0, "bad", "owner"); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := d.ApplyDirect(ctx, target, RestrictLimited, 0, "milder", "owner"); err != nil || changed {
		t.Fatalf("a limit over a ban: %v %v", changed, err)
	}
	r, err := d.Restriction(ctx, target)
	if err != nil || r.Kind != RestrictBanned {
		t.Fatalf("ban lost: %+v %v", r, err)
	}
}

func TestExpiredRestrictionReadsAsNone(t *testing.T) {
	d, db := newIntegrationDAO(t)
	ctx := context.Background()
	target := nextUser()
	if _, err := d.ApplyDirect(ctx, target, RestrictBanned, int32(time.Now().Unix()-1), "old", "owner"); err != nil {
		t.Fatal(err)
	}
	r, err := d.Restriction(ctx, target)
	if err != nil || r != nil {
		t.Errorf("expired: %+v %v", r, err)
	}
	phone := fmt.Sprintf("%d", target)
	if _, err = db.Exec(ctx, "INSERT INTO users (id, phone, first_name, last_name, username, country_code) VALUES (?, ?, 'U', '', '', '')", target, phone); err != nil {
		t.Fatal(err)
	}
	if banned, err := d.PhoneBanned(ctx, phone); err != nil || banned {
		t.Errorf("an expired ban: %v %v", banned, err)
	}
	if _, err := d.ApplyDirect(ctx, target, RestrictBanned, 0, "bad", "owner"); err != nil {
		t.Fatal(err)
	}
	if banned, err := d.PhoneBanned(ctx, phone); err != nil || !banned {
		t.Errorf("a ban in force: %v %v", banned, err)
	}
}

func TestEffectsClaimAndResume(t *testing.T) {
	d, _ := newIntegrationDAO(t)
	ctx := context.Background()
	id := makeReport(t, d, nextUser(), nextUser(), []int32{1}, &Snapshot{Messages: []SnapshotMessage{{Id: 1}}})
	if err := d.Decide(ctx, Decision{ReportId: id, Actor: "m", Action: ActionBan}); err != nil {
		t.Fatal(err)
	}
	ok, err := d.ClaimEffects(ctx, id)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if ok, _ = d.ClaimEffects(ctx, id); ok {
		t.Fatal("a held claim must not be taken again")
	}
	if err = d.AdvanceEffects(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	if err = d.ReleaseEffects(ctx, id, "boom"); err != nil {
		t.Fatal(err)
	}
	r, err := d.Report(ctx, id)
	if err != nil || r.EffectsStage != 1 || r.EffectsError != "boom" || r.EffectsAttempts != 1 {
		t.Fatalf("after a failed run: %+v %v", r, err)
	}
	if ok, err = d.ClaimEffects(ctx, id); err != nil || !ok {
		t.Fatalf("resume claim: %v %v", ok, err)
	}
	ids, err := d.PendingEffects(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ids {
		if x == id {
			t.Error("a claimed report must not be pending")
		}
	}
	if err = d.AdvanceEffects(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	if err = d.ReleaseEffects(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	if r, _ = d.Report(ctx, id); r.EffectsStage != 2 || r.EffectsError != "" {
		t.Errorf("finished: %+v", r)
	}
	if ok, _ = d.ClaimEffects(ctx, id); ok {
		t.Error("a finished decision must not be claimed")
	}
}
