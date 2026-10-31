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

// Package readmarks holds the limits of read dates, when a user read a
// message of another, that several services apply. help.getAppConfig gives
// the clients the same numbers (core.telegram.org/api/config), by which
// they offer the dates.
package readmarks

const (
	// ChatExpirePeriod is chat_read_mark_expire_period: the dates of a
	// group's message are kept this many seconds after it was sent.
	ChatExpirePeriod = 7 * 24 * 60 * 60
	// ChatSizeThreshold is chat_read_mark_size_threshold: groups of up to
	// this many members tell who read a message.
	ChatSizeThreshold = 100
	// PrivateExpirePeriod is pm_read_date_expire_period: the date of a
	// private chat's message is kept this many seconds after it was sent.
	PrivateExpirePeriod = 7 * 24 * 60 * 60

	// KeepPeriod is how long a recorded read date is kept: a message is
	// read after it is sent, so a date older than this belongs to a
	// message older than either period.
	KeepPeriod = max(ChatExpirePeriod, PrivateExpirePeriod)
)

// TooOld tells whether a message sent at date (unix seconds) is too old at
// now for its read dates, period being ChatExpirePeriod or
// PrivateExpirePeriod.
func TooOld(date, now, period int64) bool {
	return now-date > period
}
