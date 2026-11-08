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

// Package historyttl has what the auto-deletion of messages shares across
// services (task 063).
package historyttl

import "github.com/teamgram/proto/mtproto"

const (
	// Min is the shortest period the clients offer: Android picks periods
	// in minutes (AutoDeletePopupWrapper, timeInMinutes * 60).
	Min int32 = 60
	// Max is the longest one: 365 days in Android's list
	// (AlertsCreator.createAutoDeleteDatePickerDialog), with the leap day.
	Max int32 = 366 * 86400
)

// Valid tells whether period is a period a chat can have: 0 turns the
// auto-deletion off (core.telegram.org/constructor/messageActionSetMessagesTTL).
// TTL_PERIOD_INVALID is the answer to any other.
func Valid(period int32) bool {
	return period == 0 || period >= Min && period <= Max
}

// IsChange tells whether m is the service message of a change of the
// period. It carries no ttl_period itself.
func IsChange(m *mtproto.Message) bool {
	return m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionSetMessagesTTL
}

// Update is updatePeerHistoryTTL of peer; a period of 0 leaves ttl_period
// out, which the clients read as off (Android TL_updatePeerHistoryTTL,
// tdesktop api_updates.cpp mtpc_updatePeerHistoryTTL).
func Update(peer *mtproto.Peer, period int32) *mtproto.Update {
	return mtproto.MakeTLUpdatePeerHistoryTTL(&mtproto.Update{
		Peer_PEER: peer,
		TtlPeriod: mtproto.MakeFlagsInt32(period),
	}).To_Update()
}

// Action is the action of the service message of a change to period.
// autoSettingFrom is the user whose default period set it, 0 for a change
// by hand.
func Action(period int32, autoSettingFrom int64) *mtproto.MessageAction {
	return mtproto.MakeTLMessageActionSetMessagesTTL(&mtproto.MessageAction{
		Period:          period,
		AutoSettingFrom: mtproto.MakeFlagsInt64(autoSettingFrom),
	}).To_MessageAction()
}
