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

package user

import (
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
)

// NotificationSoundTextMaxLen is the most characters the title and the data
// of a notificationSoundLocal may have: user_reactions_notify_settings keeps
// them in varchar(255).
const NotificationSoundTextMaxLen = 255

// CheckReactionsNotifySettings returns SETTINGS_INVALID unless settings are
// reactionsNotifySettings the server can keep (task 020): every
// *_notify_from absent (nobody) or a reactionNotificationsFrom, a sound of
// one of the four notificationSound constructors, the title and the data of
// notificationSoundLocal valid UTF-8 of at most NotificationSoundTextMaxLen
// characters, and show_previews a Bool. The ringtone of
// notificationSoundRingtone is not checked: the server keeps no saved
// ringtones (account.getSavedRingtones).
func CheckReactionsNotifySettings(settings *mtproto.ReactionsNotifySettings) error {
	if settings.GetPredicateName() != mtproto.Predicate_reactionsNotifySettings {
		return mtproto.ErrSettingsInvalid
	}
	for _, from := range []*mtproto.ReactionNotificationsFrom{
		settings.GetMessagesNotifyFrom(),
		settings.GetStoriesNotifyFrom(),
		settings.GetPollVotesNotifyFrom(),
	} {
		if from == nil {
			continue
		}
		switch from.GetPredicateName() {
		case mtproto.Predicate_reactionNotificationsFromContacts,
			mtproto.Predicate_reactionNotificationsFromAll:
		default:
			return mtproto.ErrSettingsInvalid
		}
	}
	sound := settings.GetSound()
	switch sound.GetPredicateName() {
	case mtproto.Predicate_notificationSoundDefault,
		mtproto.Predicate_notificationSoundNone,
		mtproto.Predicate_notificationSoundRingtone:
	case mtproto.Predicate_notificationSoundLocal:
		if !soundTextValid(sound.GetTitle()) || !soundTextValid(sound.GetData()) {
			return mtproto.ErrSettingsInvalid
		}
	default:
		return mtproto.ErrSettingsInvalid
	}
	switch settings.GetShowPreviews().GetPredicateName() {
	case mtproto.Predicate_boolTrue, mtproto.Predicate_boolFalse:
	default:
		return mtproto.ErrSettingsInvalid
	}
	return nil
}

func soundTextValid(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= NotificationSoundTextMaxLen
}
