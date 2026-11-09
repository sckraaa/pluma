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

package moderation

import (
	"errors"
	"strings"
	"testing"
)

func TestRestrictionActive(t *testing.T) {
	const now = 1000
	if (*Restriction)(nil).Active(now) {
		t.Error("nil is active")
	}
	if !(&Restriction{UntilDate: 0}).Active(now) {
		t.Error("no end must hold")
	}
	if !(&Restriction{UntilDate: 1001}).Active(now) {
		t.Error("before its end must hold")
	}
	if (&Restriction{UntilDate: 1000}).Active(now) {
		t.Error("at its end must not hold")
	}
}

func TestStepsOf(t *testing.T) {
	cases := map[string]int{ActionDismiss: 0, ActionDelete: 1, ActionRestrict: 1, ActionBan: 2}
	for action, n := range cases {
		steps, err := StepsOf(action)
		if err != nil || len(steps) != n {
			t.Errorf("%s: %v %v", action, steps, err)
		}
	}
	// Telling the user before the sessions end is what lets it be read.
	if steps, _ := StepsOf(ActionBan); steps[0] != StepNotify || steps[1] != StepRevoke {
		t.Errorf("ban steps: %v", steps)
	}
	if _, err := StepsOf("unknown"); !errors.Is(err, ErrBadAction) {
		t.Errorf("unknown: %v", err)
	}
}

func TestDedupKey(t *testing.T) {
	r := &Report{ReporterUserId: 1, Kind: KindMessage, PeerType: 2, PeerId: 3, Reason: "spam"}
	k := dedupKey(r, []int32{1, 2})
	if k != dedupKey(r, []int32{1, 2}) {
		t.Error("the same report must have the same key")
	}
	if k == dedupKey(r, []int32{1, 3}) {
		t.Error("other messages must be another report")
	}
	other := *r
	other.Reason = "violence"
	if k == dedupKey(&other, []int32{1, 2}) {
		t.Error("another reason must be another report")
	}
	if len(k) != 64 {
		t.Errorf("key of %d chars", len(k))
	}
}

func TestTruncateComment(t *testing.T) {
	long := strings.Repeat("я", MaxCommentChars+5)
	if got := TruncateComment(long); len([]rune(got)) != MaxCommentChars {
		t.Errorf("%d chars", len([]rune(got)))
	}
	if got := TruncateComment("short"); got != "short" {
		t.Errorf("%q", got)
	}
}
