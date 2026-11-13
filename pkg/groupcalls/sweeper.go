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

package groupcalls

import "time"

// The collector (ADR 0059 §10) compares who is in a call by the table with
// whom the SFU holds. A participant the SFU holds is seen now; one it has
// not held for longer than the absence limit leaves the call, versioned,
// as by phone.leaveGroupCall. The limit covers a client that connects
// again: tdesktop and iOS ask phone.checkGroupCall every 4 s while they
// connect and join anew when their source is gone (tdesktop
// calls_group_call.cpp:55, 3746-3776), so a lost room (an SFU restart)
// costs a join, not a leave.

// Sweep splits the participants in a call: those whose accounts the SFU
// holds (held), to be marked seen, and those it has not held since
// now-absent, to leave. A participant who joined a moment ago counts as
// seen at the join.
func Sweep(in []*Participant, held map[int64]bool, now time.Time, absent time.Duration) (seen []int64, gone []*Participant) {
	limit := now.Add(-absent).UnixMilli()
	for _, p := range in {
		if !p.InCall {
			continue
		}
		switch {
		case held[p.UserId]:
			seen = append(seen, p.UserId)
		case p.SeenAt < limit:
			gone = append(gone, p)
		}
	}
	return seen, gone
}
