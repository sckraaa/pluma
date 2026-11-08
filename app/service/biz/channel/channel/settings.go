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
	// SettingSignatures signs the posts of a broadcast channel with their
	// authors (channels.toggleSignatures).
	SettingSignatures int32 = 1
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
)

// The values channel.editExportedChatInvite gives (fields).
const (
	InviteFieldExpireDate int32 = 1 << iota
	InviteFieldUsageLimit
	InviteFieldRequestNeeded
	InviteFieldTitle
)
