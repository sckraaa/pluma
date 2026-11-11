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

package ghost

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

func TestPositive(t *testing.T) {
	if got := positive([]int64{3, 0, -1, 3, 2}); !slices.Equal(got, []int64{3, 2}) {
		t.Errorf("positive: %v", got)
	}
	if got := positive(nil); len(got) != 0 {
		t.Errorf("positive(nil): %v", got)
	}
}

func TestPlaceholders(t *testing.T) {
	for n, want := range map[int]string{0: "", 1: "?", 3: "?, ?, ?"} {
		if got := placeholders(n); got != want {
			t.Errorf("placeholders(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestKicked: a kick runs until its date, for good at 0 and Int32.max; a
// row that left is not kicked.
func TestKicked(t *testing.T) {
	const now = 1000
	for _, c := range []struct {
		row  channelRow
		want bool
	}{
		{channelRow{State: channelperm.StateKicked}, true},
		{channelRow{State: channelperm.StateKicked, UntilDate: math.MaxInt32}, true},
		{channelRow{State: channelperm.StateKicked, UntilDate: now + 1}, true},
		{channelRow{State: channelperm.StateKicked, UntilDate: now}, false},
		{channelRow{State: channelperm.StateLeft}, false},
		{channelRow{State: channelperm.StateIn}, false},
	} {
		if got := c.row.kicked(now); got != c.want {
			t.Errorf("%+v: kicked %v, want %v", c.row, got, c.want)
		}
	}
}

// TestJoinChannelQuery: a hidden join comes back with the whole history,
// no role, rights or restriction, and the row hidden.
func TestJoinChannelQuery(t *testing.T) {
	for _, part := range []string{"hidden = 1", "available_min_id = 0", "admin_rights = 0", "banned_rights = 0", "until_date = 0", "kicked_by = 0", "`rank` = ''"} {
		if !strings.Contains(joinChannelQuery, part) {
			t.Errorf("joinChannelQuery lacks %q", part)
		}
	}
	if strings.Contains(joinChannelQuery, "last_send_date") {
		t.Error("joinChannelQuery resets the last send under the slow mode")
	}
}
