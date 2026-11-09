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
	"math"
	"testing"
)

// A cache_time from a bot is bounded, and an inline answer outlives the cache
// that points to it by inlineKeep, whatever the cache_time.
func TestCacheTimes(t *testing.T) {
	for _, tc := range []struct {
		cacheTime int32
		cache     int
	}{{-1, 0}, {0, 0}, {300, 300}, {3600, 3600}, {cacheTimeMax, cacheTimeMax}, {math.MaxInt32, cacheTimeMax}} {
		if got := cacheSeconds(tc.cacheTime); got != tc.cache {
			t.Errorf("cache_time %d: kept %d, want %d", tc.cacheTime, got, tc.cache)
		}
		if got := inlineKeepOf(tc.cacheTime); got != tc.cache+inlineKeep {
			t.Errorf("cache_time %d: answer kept %d, want %d", tc.cacheTime, got, tc.cache+inlineKeep)
		}
	}
}

// One user waits for at most maxWaitsPerUser requests; others are not held
// back, and a request that ends frees its place.
func TestWaitsPerUser(t *testing.T) {
	d := &Dao{}
	for i := 0; i < maxWaitsPerUser; i++ {
		if !d.EnterWait(1) {
			t.Fatalf("request %d refused", i)
		}
	}
	if d.EnterWait(1) {
		t.Fatal("one over the limit let in")
	}
	if !d.EnterWait(2) {
		t.Fatal("another user held back")
	}
	d.LeaveWait(1)
	if !d.EnterWait(1) {
		t.Fatal("the freed place refused")
	}
	d.LeaveWait(2)
	if _, left := d.waits[2]; left {
		t.Fatal("the counter of a user with no requests stays")
	}
}
