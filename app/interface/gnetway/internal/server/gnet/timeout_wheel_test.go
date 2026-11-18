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

package gnet

import (
	"slices"
	"testing"
)

// ExpireThrough expires every second since its previous call, so a late
// tick loses no connection; ExpireSlot of the timeout wheel is unchanged.
func TestTimeoutWheelExpireThrough(t *testing.T) {
	tw := newPingWheel()
	const now = int64(1_800_000_000)
	tw.Add(1, now+1)
	tw.Add(2, now+2)
	tw.Add(3, now+3)
	tw.Add(4, now+70) // the slot of now+6 a lap later

	if got := tw.ExpireThrough(now); got != nil {
		t.Fatalf("at now: %v", got)
	}
	got := tw.ExpireThrough(now + 2) // now+1 was skipped
	slices.Sort(got)
	if !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("through now+2: %v, want [1 2]", got)
	}
	if got := tw.ExpireThrough(now + 2); got != nil {
		t.Fatalf("the same second again: %v", got)
	}
	// A clock that goes back by a second finds its slot already expired.
	if got := tw.ExpireThrough(now + 1); got != nil {
		t.Fatalf("an earlier second: %v", got)
	}
	// After a long stop every slot is visited once: connection 4 comes
	// early and is put back by its owner (Server.expirePings).
	got = tw.ExpireThrough(now + 1000)
	slices.Sort(got)
	if !slices.Equal(got, []int64{3, 4}) {
		t.Fatalf("after a long stop: %v, want [3 4]", got)
	}

	// After the clock went back an hour, the slots of the new seconds
	// are expired at once, not an hour later.
	tw.Add(5, now-3600+2)
	if got := tw.ExpireThrough(now - 3600); got != nil {
		t.Fatalf("an hour back: %v", got)
	}
	if got := tw.ExpireThrough(now - 3600 + 2); !slices.Equal(got, []int64{5}) {
		t.Fatalf("two seconds after the clock went back: %v, want [5]", got)
	}

	w := newTimeoutWheel()
	w.Add(7, now+305)
	if got := w.ExpireSlot(now + 300); !slices.Equal(got, []int64{7}) {
		t.Fatalf("the 10-second slot of now+300: %v, want [7]", got)
	}
	if got := w.ExpireSlot(now + 300); got != nil {
		t.Fatalf("an expired slot again: %v", got)
	}
}
