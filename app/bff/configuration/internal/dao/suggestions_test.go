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
	"slices"
	"testing"
)

// BIRTHDAY_SETUP is pending until the account has a birthday or dismissed
// the suggestion.
func TestPendingSuggestions(t *testing.T) {
	for _, c := range []struct {
		hasBirthday bool
		dismissed   []string
		want        []string
	}{
		{false, nil, []string{"BIRTHDAY_SETUP"}},
		{false, []string{"BIRTHDAY_CONTACTS_TODAY"}, []string{"BIRTHDAY_SETUP"}},
		{false, []string{"BIRTHDAY_SETUP"}, []string{}},
		{true, nil, []string{}},
	} {
		if got := PendingSuggestions(c.hasBirthday, c.dismissed); !slices.Equal(got, c.want) {
			t.Errorf("birthday %v, dismissed %v: %v, want %v", c.hasBirthday, c.dismissed, got, c.want)
		}
	}
}

func TestValidSuggestion(t *testing.T) {
	for name, want := range map[string]bool{
		"BIRTHDAY_CONTACTS_TODAY": true,
		"PREMIUM_GRACE":           true,
		"":                        false,
		"birthday_setup":          false,
		"BIRTHDAY SETUP":          false,
	} {
		if got := ValidSuggestion(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
}
