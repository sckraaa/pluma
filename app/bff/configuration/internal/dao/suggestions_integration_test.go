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

package dao

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var itDao *Dao

func TestMain(m *testing.M) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conf := db.Config()
	itDao = &Dao{db: sqlx.NewMySQL(&conf)}
	code := m.Run()
	_ = db.Terminate(ctx)
	os.Exit(code)
}

// A dismissed suggestion stays dismissed, the birthday banner of contacts
// for the rest of the day of the client that asks; dismissing it again
// starts a new day.
func TestDismissedSuggestions(t *testing.T) {
	ctx := context.Background()
	const user = int64(4001)
	moscow := time.FixedZone("", 3*3600)
	newYork := time.FixedZone("", -5*3600)
	// 20:00 in Moscow, 13:00 in New York, 17:00 UTC.
	at := time.Date(2026, time.October, 31, 20, 0, 0, 0, moscow)
	for _, s := range []string{SuggestionBirthdaySetup, SuggestionBirthdayContactsToday} {
		if err := itDao.DismissSuggestion(ctx, user, s, at); err != nil {
			t.Fatal(err)
		}
	}
	check := func(now time.Time, zone *time.Location, want ...string) {
		t.Helper()
		got, err := itDao.DismissedSuggestions(ctx, user, now, zone)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("at %s: dismissed %v, want %v", now.In(zone).Format(time.RFC3339), got, want)
		}
	}
	both := []string{SuggestionBirthdayContactsToday, SuggestionBirthdaySetup}
	// Until the midnight of the client that asks.
	check(time.Date(2026, time.October, 31, 23, 59, 59, 0, moscow), moscow, both...)
	check(time.Date(2026, time.November, 1, 0, 0, 0, 0, moscow), moscow, SuggestionBirthdaySetup)
	check(time.Date(2026, time.October, 31, 23, 59, 59, 0, newYork), newYork, both...)
	check(time.Date(2026, time.November, 1, 0, 0, 0, 0, newYork), newYork, SuggestionBirthdaySetup)
	check(time.Date(2026, time.October, 31, 23, 59, 59, 0, time.UTC), time.UTC, both...)
	check(time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), time.UTC, SuggestionBirthdaySetup)
	// A dismissal the next day lasts for that day.
	next := time.Date(2026, time.November, 1, 9, 0, 0, 0, moscow)
	if err := itDao.DismissSuggestion(ctx, user, SuggestionBirthdayContactsToday, next); err != nil {
		t.Fatal(err)
	}
	check(next.Add(time.Hour), moscow, both...)
	check(time.Date(2026, time.November, 2, 0, 0, 0, 0, moscow), moscow, SuggestionBirthdaySetup)
	// Another account dismissed nothing.
	got, err := itDao.DismissedSuggestions(ctx, user+1, at, time.UTC)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("another account: %v %v", got, err)
	}
}
