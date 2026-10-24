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

package core

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Premium granted by the admin on MySQL and Redis (task 014).

// premiumPushes returns the payloads of the pushes to userId written so far.
func premiumPushes(t *testing.T, userId int64) []string {
	t.Helper()
	rows, err := itRaw.Query("SELECT payload FROM mq_outbox WHERE recipient_id = ? AND topic = 'Sync-T' AND payload LIKE '%updateUser%' ORDER BY id", userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// premiumOf returns userId's Premium as the service serves the user now.
func premiumOf(t *testing.T, userId int64) bool {
	t.Helper()
	users, err := New(context.Background(), itSvc).UserGetMutableUsers(&user.TLUserGetMutableUsers{Id: []int64{userId}})
	if err != nil {
		t.Fatal(err)
	}
	me, ok := users.GetImmutableUser(userId)
	if !ok {
		t.Fatalf("user %d not loaded", userId)
	}
	return me.Premium()
}

func TestPremiumGrantAndRevoke(t *testing.T) {
	ctx := context.Background()
	u, _ := newUser(t, "Paula")
	if premiumOf(t, u) {
		t.Fatal("a new user has Premium")
	}

	state, err := ChangePremium(ctx, itSvc, u, dao.PremiumChange{Grant: true})
	if err != nil {
		t.Fatal(err)
	}
	if state != (dao.PremiumState{Premium: true}) || !premiumOf(t, u) {
		t.Fatalf("for good: %+v, served %v", state, premiumOf(t, u))
	}
	pushes := premiumPushes(t, u)
	if len(pushes) != 1 || !strings.Contains(pushes[0], `"premium":true`) {
		t.Fatalf("pushes after the grant: %q", pushes)
	}

	// The same grant again changes nothing and pushes nothing.
	if _, err = ChangePremium(ctx, itSvc, u, dao.PremiumChange{Grant: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(premiumPushes(t, u)); n != 1 {
		t.Fatalf("%d pushes after a grant that changed nothing", n)
	}

	before := time.Now()
	state, err = ChangePremium(ctx, itSvc, u, dao.PremiumChange{Grant: true, Months: 2})
	if err != nil {
		t.Fatal(err)
	}
	end := time.Unix(state.Until, 0)
	if !state.Premium || end.Before(before.AddDate(0, 2, -1)) || end.After(time.Now().AddDate(0, 2, 1)) {
		t.Fatalf("two months: %+v", state)
	}

	if state, err = ChangePremium(ctx, itSvc, u, dao.PremiumChange{}); err != nil || state != (dao.PremiumState{}) {
		t.Fatalf("revoke: %+v, %v", state, err)
	}
	if premiumOf(t, u) {
		t.Fatal("Premium served after the revoke: a stale cache")
	}
	if pushes = premiumPushes(t, u); len(pushes) != 3 || strings.Contains(pushes[2], `"premium":true`) {
		t.Fatalf("pushes after the revoke: %d, last %q", len(pushes), pushes[len(pushes)-1])
	}
}

func TestPremiumExpiry(t *testing.T) {
	ctx := context.Background()
	u, _ := newUser(t, "Ernest")
	running, _ := newUser(t, "Rita")
	if _, err := ChangePremium(ctx, itSvc, u, dao.PremiumChange{Grant: true, Months: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := ChangePremium(ctx, itSvc, running, dao.PremiumChange{Grant: true, Months: 1}); err != nil {
		t.Fatal(err)
	}
	// The month of u is over.
	if _, err := itRaw.Exec("UPDATE users SET premium_expire_date = ? WHERE id = ?", time.Now().Unix()-10, u); err != nil {
		t.Fatal(err)
	}

	ids, err := itSvc.Dao.ExpiredPremium(ctx, time.Now().Unix(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids, u) || slices.Contains(ids, running) {
		t.Fatalf("expired %v: want %d and not %d", ids, u, running)
	}

	w := NewPremiumWorker(itSvc)
	w.sweep(ctx)
	ids, err = itSvc.Dao.ExpiredPremium(ctx, time.Now().Unix(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ids, u) {
		t.Fatal("still expired after a sweep")
	}
	if pushes := premiumPushes(t, u); len(pushes) != 2 || strings.Contains(pushes[1], `"premium":true`) {
		t.Fatalf("pushes of the expiry: %q", pushes)
	}
	if !premiumOf(t, running) {
		t.Fatal("a running grant was taken away")
	}
}
