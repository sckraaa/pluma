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

package dao

import (
	"context"
	"regexp"
	"slices"
	"time"
)

// Suggestions are the tooltips a client shows by help.getPromoData
// (core.telegram.org/api/config, "Suggestions").
const (
	// SuggestionBirthdaySetup asks the user to set their birthday.
	SuggestionBirthdaySetup = "BIRTHDAY_SETUP"
	// SuggestionBirthdayContactsToday is the banner of contacts' birthdays.
	// It is inverted: on until dismissed, and never pending.
	SuggestionBirthdayContactsToday = "BIRTHDAY_CONTACTS_TODAY"
)

// DayLong is how long a dismissal of BIRTHDAY_CONTACTS_TODAY lasts: the
// banner is about the birthdays of the day before, the day and the day after
// (contacts.getBirthdays), and comes back for the next ones. Telegram drops
// the banner when the user sent a gift to a contact whose birthday is
// "within the next/previous 24 hours" (core.telegram.org/api/config).
const DayLong = 24 * time.Hour

// DismissSuggestion records that userId dismissed suggestion at now.
func (d *Dao) DismissSuggestion(ctx context.Context, userId int64, suggestion string, now time.Time) error {
	_, err := d.db.Exec(ctx,
		"INSERT INTO user_dismissed_suggestions (user_id, suggestion, date2) VALUES (?, ?, ?) AS new ON DUPLICATE KEY UPDATE date2 = new.date2",
		userId, suggestion, now.Unix())
	return err
}

// DismissedSuggestions returns the suggestions userId has dismissed, by
// name: BIRTHDAY_CONTACTS_TODAY only for DayLong after its dismissal.
func (d *Dao) DismissedSuggestions(ctx context.Context, userId int64, now time.Time) ([]string, error) {
	var rows []struct {
		Suggestion string `db:"suggestion"`
		Date       int64  `db:"date2"`
	}
	if err := d.db.QueryRowsPartial(ctx, &rows, "SELECT suggestion, date2 FROM user_dismissed_suggestions WHERE user_id = ? ORDER BY suggestion", userId); err != nil {
		return nil, err
	}
	dismissed := []string{}
	for _, r := range rows {
		if r.Suggestion == SuggestionBirthdayContactsToday && now.Sub(time.Unix(r.Date, 0)) >= DayLong {
			continue
		}
		dismissed = append(dismissed, r.Suggestion)
	}
	return dismissed, nil
}

// suggestionName is the shape of the documented suggestion names.
var suggestionName = regexp.MustCompile(`^[A-Z0-9_]{1,64}$`)

// ValidSuggestion tells whether name can be a suggestion.
func ValidSuggestion(name string) bool {
	return suggestionName.MatchString(name)
}

// PendingSuggestions are the suggestions a client should show: setting a
// birthday, until the account has one or dismissed the suggestion.
func PendingSuggestions(hasBirthday bool, dismissed []string) []string {
	pending := []string{}
	if !hasBirthday && !slices.Contains(dismissed, SuggestionBirthdaySetup) {
		pending = append(pending, SuggestionBirthdaySetup)
	}
	return pending
}
