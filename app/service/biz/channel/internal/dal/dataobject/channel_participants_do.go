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

package dataobject

// ChannelParticipantsDO is a row of channel_participants
// (migrate-20261103-channels.sql), the columns the service reads.
type ChannelParticipantsDO struct {
	ChannelId      int64  `db:"channel_id" json:"channel_id"`
	UserId         int64  `db:"user_id" json:"user_id"`
	Role           int32  `db:"role" json:"role"`
	State          int32  `db:"state" json:"state"`
	AdminRights    int32  `db:"admin_rights" json:"admin_rights"`
	Rank           string `db:"rank" json:"rank"`
	BannedRights   int32  `db:"banned_rights" json:"banned_rights"`
	UntilDate      int32  `db:"until_date" json:"until_date"`
	InviterUserId  int64  `db:"inviter_user_id" json:"inviter_user_id"`
	PromotedBy     int64  `db:"promoted_by" json:"promoted_by"`
	KickedBy       int64  `db:"kicked_by" json:"kicked_by"`
	AvailableMinId int32  `db:"available_min_id" json:"available_min_id"`
	Date           int64  `db:"date" json:"date"`
}
