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

package core

import (
	"unicode/utf8"
)

// The most characters of the bio: help.getAppConfig gives the same numbers
// (about_length_limit_default, about_length_limit_premium), by which the
// clients limit the field.
const (
	AboutLimit        = 70
	AboutLimitPremium = 70
)

// aboutTooLong tells whether about is longer than the limit. Characters
// are Unicode code points: Android and Web K count UTF-16 code units and
// iOS grapheme clusters (PeerInfoScreen.swift, bio.count), so a bio a
// client lets through passes unless iOS took emoji of several code points.
func aboutTooLong(about string, premium bool) bool {
	limit := AboutLimit
	if premium {
		limit = AboutLimitPremium
	}
	return utf8.RuneCountInString(about) > limit
}
