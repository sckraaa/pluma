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

package channelperm

import "testing"

func TestSlowmodeValid(t *testing.T) {
	for _, s := range []int32{0, 5, 10, 30, 60, 300, 900, 3600} {
		if !SlowmodeValid(s) {
			t.Errorf("%d seconds refused", s)
		}
	}
	for _, s := range []int32{-1, 1, 7, 45, 600, 3601} {
		if SlowmodeValid(s) {
			t.Errorf("%d seconds accepted", s)
		}
	}
}

func TestSlowmodeApplies(t *testing.T) {
	if !SlowmodeApplies(30, RoleMember) {
		t.Error("a member is not limited")
	}
	if SlowmodeApplies(0, RoleMember) {
		t.Error("a member is limited with the slow mode off")
	}
	if SlowmodeApplies(30, RoleAdmin) || SlowmodeApplies(30, RoleCreator) {
		t.Error("an admin or the creator is limited")
	}
}

func TestSlowmodeWait(t *testing.T) {
	const now = 1000
	for _, c := range []struct {
		name    string
		seconds int32
		last    int64
		wait    int32
		next    int64
	}{
		{"never sent", 30, 0, 0, 0},
		{"off", 0, 990, 0, 0},
		{"inside the window", 30, 990, 20, 1020},
		{"one second before the boundary", 30, 971, 1, 1001},
		{"at the boundary", 30, 970, 0, 0},
		{"past the boundary", 30, 900, 0, 0},
		{"interval made longer", 60, 970, 30, 1030},
		{"interval made shorter", 10, 970, 0, 0},
		{"a clock gone back", 30, 1500, 30, 1030},
	} {
		if got := SlowmodeWait(c.seconds, c.last, now); got != c.wait {
			t.Errorf("%s: wait %d, want %d", c.name, got, c.wait)
		}
		if got := SlowmodeNextSend(c.seconds, c.last, now); got != c.next {
			t.Errorf("%s: next %d, want %d", c.name, got, c.next)
		}
	}
}
