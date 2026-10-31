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
// for a day; dismissing it again starts the day again.
func TestDismissedSuggestions(t *testing.T) {
	ctx := context.Background()
	const user = int64(4001)
	now := time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)
	for _, s := range []string{SuggestionBirthdaySetup, SuggestionBirthdayContactsToday} {
		if err := itDao.DismissSuggestion(ctx, user, s, now); err != nil {
			t.Fatal(err)
		}
	}
	check := func(at time.Time, want ...string) {
		t.Helper()
		got, err := itDao.DismissedSuggestions(ctx, user, at)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("at %s: dismissed %v, want %v", at.Format(time.RFC3339), got, want)
		}
	}
	check(now.Add(DayLong-time.Second), SuggestionBirthdayContactsToday, SuggestionBirthdaySetup)
	check(now.Add(DayLong), SuggestionBirthdaySetup)
	if err := itDao.DismissSuggestion(ctx, user, SuggestionBirthdayContactsToday, now.Add(DayLong)); err != nil {
		t.Fatal(err)
	}
	check(now.Add(DayLong+time.Hour), SuggestionBirthdayContactsToday, SuggestionBirthdaySetup)
	check(now.Add(DayLong), SuggestionBirthdayContactsToday, SuggestionBirthdaySetup)
	// Another account dismissed nothing.
	got, err := itDao.DismissedSuggestions(ctx, user+1, now)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("another account: %v %v", got, err)
	}
}
