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

import "database/sql"

// ChannelsDO is a row of channels (migrate-20261103-channels.sql), the
// columns the service reads.
type ChannelsDO struct {
	Id                  int64  `db:"id" json:"id"`
	AccessHash          int64  `db:"access_hash" json:"access_hash"`
	CreatorUserId       int64  `db:"creator_user_id" json:"creator_user_id"`
	Title               string `db:"title" json:"title"`
	About               string `db:"about" json:"about"`
	PhotoId             int64  `db:"photo_id" json:"photo_id"`
	Broadcast           bool   `db:"broadcast" json:"broadcast"`
	Megagroup           bool   `db:"megagroup" json:"megagroup"`
	Forum               bool   `db:"forum" json:"forum"`
	ForumTabs           bool   `db:"forum_tabs" json:"forum_tabs"`
	Signatures          bool   `db:"signatures" json:"signatures"`
	SignatureProfiles   bool   `db:"signature_profiles" json:"signature_profiles"`
	Pts                 int32  `db:"pts" json:"pts"`
	TopMessage          int32  `db:"top_message" json:"top_message"`
	ParticipantsCount   int32  `db:"participants_count" json:"participants_count"`
	AdminsCount         int32  `db:"admins_count" json:"admins_count"`
	KickedCount         int32  `db:"kicked_count" json:"kicked_count"`
	BannedCount         int32  `db:"banned_count" json:"banned_count"`
	DefaultBannedRights int32  `db:"default_banned_rights" json:"default_banned_rights"`
	SlowmodeSeconds     int32  `db:"slowmode_seconds" json:"slowmode_seconds"`
	HiddenPrehistory    bool   `db:"hidden_prehistory" json:"hidden_prehistory"`
	LinkedChatId        int64  `db:"linked_chat_id" json:"linked_chat_id"`
	MigratedFromChatId  int64  `db:"migrated_from_chat_id" json:"migrated_from_chat_id"`
	MigratedFromMaxId   int32  `db:"migrated_from_max_id" json:"migrated_from_max_id"`
	Noforwards          bool   `db:"noforwards" json:"noforwards"`
	JoinToSend          bool   `db:"join_to_send" json:"join_to_send"`
	JoinRequest         bool   `db:"join_request" json:"join_request"`
	// The reactions the members may use (task 044): the kind, and the
	// keys of some (a JSON array, NULL for none).
	AvailableReactionsType int32          `db:"available_reactions_type" json:"available_reactions_type"`
	AvailableReactions     sql.NullString `db:"available_reactions" json:"available_reactions"`
	ParticipantsHidden     bool           `db:"participants_hidden" json:"participants_hidden"`
	Deleted                bool           `db:"deleted" json:"deleted"`
	Blocked                bool           `db:"blocked" json:"blocked"`
	Version                int32          `db:"version" json:"version"`
	Date                   int64          `db:"date" json:"date"`
}
