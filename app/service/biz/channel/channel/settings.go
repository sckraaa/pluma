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

package channel

// The settings channel.toggleSetting turns on and off.
const (
	// SettingPrehistoryHidden hides the history of a supergroup from new
	// members (channels.togglePreHistoryHidden).
	SettingPrehistoryHidden int32 = 2
	// SettingParticipantsHidden hides the members of a supergroup from
	// members who are not admins (channels.toggleParticipantsHidden).
	SettingParticipantsHidden int32 = 3
	// SettingNoforwards forbids forwarding and saving the messages
	// (messages.toggleNoForwards).
	SettingNoforwards int32 = 4
	// SettingJoinToSend asks the commenters of a discussion group to join it
	// first (channels.toggleJoinToSend).
	SettingJoinToSend int32 = 5
	// SettingGigagroup turns a supergroup into a broadcast group for good
	// (channels.convertToGigagroup): only on, by its creator.
	SettingGigagroup int32 = 6
	// SettingAntispam turns the antispam of a supergroup on and off
	// (channels.toggleAntiSpam, task 082), by one who may delete the
	// messages of others.
	SettingAntispam int32 = 7
	// SettingAutotranslation translates a broadcast channel for all its
	// subscribers (channels.toggleAutotranslation, task 082).
	SettingAutotranslation int32 = 8
)

// MembersMax is the most members of a supergroup or channel: the
// megagroup_size_max help.getConfig gives (teamgramd/bin/config.json), as
// Telegram's (core.telegram.org/api/config: 200000). The clients offer a
// broadcast group from 1000 under it.
const MembersMax = 200000

// The values channel.editExportedChatInvite gives (fields).
const (
	InviteFieldExpireDate int32 = 1 << iota
	InviteFieldUsageLimit
	InviteFieldRequestNeeded
	InviteFieldTitle
)
