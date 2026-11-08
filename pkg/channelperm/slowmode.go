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

// The slow mode of a supergroup (task 045): a member sends one message every
// so many seconds, a whole album or a forwarded album being one.

// SlowmodeValid reports whether seconds is an interval of the slow mode, 0
// being none: the values every official client offers (tdesktop
// edit_peer_permissions_box.cpp:216-229, Android
// ChatUsersActivity.java:2113-2130, iOS ChatSlowmodeItem.swift:64).
func SlowmodeValid(seconds int32) bool {
	switch seconds {
	case 0, 5, 10, 30, 60, 5 * 60, 15 * 60, 60 * 60:
		return true
	}
	return false
}

// SlowmodeApplies reports whether the slow mode limits a participant of role:
// never the creator or an admin (tdesktop data_peer.cpp:1831-1837, Android
// ChatObject.java:2119-2120, iOS TelegramChannel.swift:302-312, Web K
// appMessagesManager.ts:9228).
func SlowmodeApplies(seconds, role int32) bool {
	return seconds > 0 && role == RoleMember
}

// SlowmodeNextSend is when a member whose last message under the slow mode
// was at last may send again; 0 when it may send at now. A clock that went
// back never makes the wait longer than the interval.
func SlowmodeNextSend(seconds int32, last, now int64) int64 {
	if seconds <= 0 || last <= 0 {
		return 0
	}
	next := min(last, now) + int64(seconds)
	if next <= now {
		return 0
	}
	return next
}

// SlowmodeWait is the seconds a member must wait to send at now, 0 for none.
func SlowmodeWait(seconds int32, last, now int64) int32 {
	if next := SlowmodeNextSend(seconds, last, now); next != 0 {
		return int32(next - now)
	}
	return 0
}
