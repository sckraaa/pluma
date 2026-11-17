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
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// mutes counts in each bucket of b the members who muted the channel and
// those who unmuted it, from the changes of their muting in effect
// (pkg/channelstats MuteChanged), by user and in order. A muting for a
// time that ran out before end is an unmuting at its time. A row is
// written only when the muting in effect changed, so the first row of a
// member is a muting or an unmuting by itself; after it, a muting until
// another time is no new muting.
func mutes(b statsgraph.Buckets, events []dao.MuteEvent, end int64) (muted, unmuted []int64) {
	muted, unmuted = make([]int64, b.Len()), make([]int64, b.Len())
	add := func(to []int64, t int64) {
		if j, ok := b.IndexOf(t); ok {
			to[j]++
		}
	}
	for i := 0; i < len(events); {
		user := events[i].UserId
		var (
			known, isMuted bool
			until          int64
		)
		for ; i < len(events) && events[i].UserId == user; i++ {
			e := events[i]
			if isMuted && until <= e.Date {
				add(unmuted, until)
				isMuted = false
			}
			nowMuted := int64(e.MuteUntil) > e.Date
			switch {
			case !known && nowMuted, known && nowMuted && !isMuted:
				add(muted, e.Date)
			case !known && !nowMuted, known && !nowMuted && isMuted:
				add(unmuted, e.Date)
			}
			known, isMuted, until = true, nowMuted, int64(e.MuteUntil)
		}
		if isMuted && until < end {
			add(unmuted, until)
		}
	}
	return muted, unmuted
}
