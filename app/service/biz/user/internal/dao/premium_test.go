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
	"testing"
	"time"
)

func TestPremiumChange(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC).Unix()
	months := func(from int64, n int) int64 { return time.Unix(from, 0).AddDate(0, n, 0).Unix() }
	later := months(now, 2)
	for _, tc := range []struct {
		name   string
		cur    PremiumState
		change PremiumChange
		want   PremiumState
	}{
		{"for good, from none", PremiumState{}, PremiumChange{Grant: true}, PremiumState{Premium: true}},
		{"for good, from months", PremiumState{Premium: true, Until: later}, PremiumChange{Grant: true}, PremiumState{Premium: true}},
		{"months, from none", PremiumState{}, PremiumChange{Grant: true, Months: 3}, PremiumState{Premium: true, Until: months(now, 3)}},
		{"months, added to running ones", PremiumState{Premium: true, Until: later}, PremiumChange{Grant: true, Months: 1}, PremiumState{Premium: true, Until: months(later, 1)}},
		{"months, after ended ones", PremiumState{Premium: true, Until: now - 1}, PremiumChange{Grant: true, Months: 1}, PremiumState{Premium: true, Until: months(now, 1)}},
		{"months, from Premium for good", PremiumState{Premium: true}, PremiumChange{Grant: true, Months: 1}, PremiumState{Premium: true, Until: months(now, 1)}},
		{"taken away", PremiumState{Premium: true}, PremiumChange{}, PremiumState{}},
		{"expiry of ended months", PremiumState{Premium: true, Until: now}, PremiumChange{OnlyExpired: true}, PremiumState{}},
		{"expiry of running months", PremiumState{Premium: true, Until: later}, PremiumChange{OnlyExpired: true}, PremiumState{Premium: true, Until: later}},
		{"expiry of Premium for good", PremiumState{Premium: true}, PremiumChange{OnlyExpired: true}, PremiumState{Premium: true}},
	} {
		if got := tc.change.apply(tc.cur, now); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if (PremiumState{Premium: true, Until: now}).Active(now) || !(PremiumState{Premium: true}).Active(now) {
		t.Error("Active")
	}
}
