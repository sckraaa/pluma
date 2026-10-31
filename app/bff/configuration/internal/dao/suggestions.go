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
	"math"
	"regexp"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/jsonx"
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

// A dismissal of BIRTHDAY_CONTACTS_TODAY lasts for the rest of the user's
// day: the banner is about today's birthdays and comes back for the next
// day's. The iOS and desktop apps show it on every launch unless the server
// lists it as dismissed (Telegram-iOS GlobalControlPanelsContext.swift:390,
// tdesktop PromoSuggestions::current), so the server has to end the
// dismissal at the user's midnight; Android and Web K also remember a close
// for the day themselves (BirthdayController.hide, appPromoManager). The day
// is the one of the client that asks: its initConnection params carry its
// offset from UTC (ClientZone).

// minZoneOffset and maxZoneOffset bound the tz_offset taken from a client, in
// seconds: the time zones in use run from UTC-12 to UTC+14, as tdesktop
// slices the offset it sends (SessionPrivate::prepareInitParams).
const (
	minZoneOffset = -12 * 3600
	maxZoneOffset = 14 * 3600
)

// ClientZone is the time zone of a client by the tz_offset of its
// initConnection params, as the session stores them (a JSONValue), in
// seconds east of UTC: Android sends the default zone's offset with daylight
// saving (ConnectionsManager.java:253, ConnectionsManager.cpp:3192), iOS
// secondsFromGMT (BuildConfig.m:154-155), tdesktop the local offset rounded
// to 15 minutes (session_private.cpp:550-570). A client that sends none, as
// Web K, or one out of range, is taken to be on UTC.
func ClientZone(params string) *time.Location {
	if params == "" || params == "null" {
		return time.UTC
	}
	var v mtproto.JSONValue
	if err := jsonx.UnmarshalFromString(params, &v); err != nil {
		return time.UTC
	}
	for _, kv := range v.GetValue_VECTORJSONOBJECTVALUE() {
		if kv.GetKey() != "tz_offset" || kv.GetValue().GetPredicateName() != mtproto.Predicate_jsonNumber {
			continue
		}
		offset := kv.GetValue().GetValue_FLOAT64()
		if math.IsNaN(offset) || offset < minZoneOffset || offset > maxZoneOffset {
			return time.UTC
		}
		return time.FixedZone("", int(offset))
	}
	return time.UTC
}

// sameDay tells whether a and b fall on the same date in zone.
func sameDay(a, b time.Time, zone *time.Location) bool {
	ya, ma, da := a.In(zone).Date()
	yb, mb, db := b.In(zone).Date()
	return ya == yb && ma == mb && da == db
}

// DismissSuggestion records that userId dismissed suggestion at now.
func (d *Dao) DismissSuggestion(ctx context.Context, userId int64, suggestion string, now time.Time) error {
	_, err := d.db.Exec(ctx,
		"INSERT INTO user_dismissed_suggestions (user_id, suggestion, date2) VALUES (?, ?, ?) AS new ON DUPLICATE KEY UPDATE date2 = new.date2",
		userId, suggestion, now.Unix())
	return err
}

// DismissedSuggestions returns the suggestions userId has dismissed, by
// name: BIRTHDAY_CONTACTS_TODAY only on the day of its dismissal in zone, the
// time zone of the client that asks.
func (d *Dao) DismissedSuggestions(ctx context.Context, userId int64, now time.Time, zone *time.Location) ([]string, error) {
	var rows []struct {
		Suggestion string `db:"suggestion"`
		Date       int64  `db:"date2"`
	}
	if err := d.db.QueryRowsPartial(ctx, &rows, "SELECT suggestion, date2 FROM user_dismissed_suggestions WHERE user_id = ? ORDER BY suggestion", userId); err != nil {
		return nil, err
	}
	dismissed := []string{}
	for _, r := range rows {
		if r.Suggestion == SuggestionBirthdayContactsToday && !sameDay(time.Unix(r.Date, 0), now, zone) {
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
