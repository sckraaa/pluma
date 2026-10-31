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

package dao

import (
	"context"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Who a reaction notification comes from, as user_reactions_notify_settings
// keeps it: an absent *_notify_from field is nobody.
const (
	reactionsFromNobody   = 0
	reactionsFromContacts = 1
	reactionsFromAll      = 2
)

// The kinds of the sound of reaction notifications, as
// user_reactions_notify_settings keeps them.
const (
	soundKindDefault  = 0
	soundKindNone     = 1
	soundKindLocal    = 2
	soundKindRingtone = 3
)

// reactionsNotifySettingsRow is a row of user_reactions_notify_settings.
type reactionsNotifySettingsRow struct {
	MessagesNotifyFrom  int32  `db:"messages_notify_from"`
	StoriesNotifyFrom   int32  `db:"stories_notify_from"`
	PollVotesNotifyFrom int32  `db:"poll_votes_notify_from"`
	SoundKind           int32  `db:"sound_kind"`
	SoundTitle          string `db:"sound_title"`
	SoundData           string `db:"sound_data"`
	SoundRingtoneId     int64  `db:"sound_ringtone_id"`
	ShowPreviews        bool   `db:"show_previews"`
}

const reactionsNotifySettingsColumns = "messages_notify_from, stories_notify_from, poll_votes_notify_from, sound_kind, sound_title, sound_data, sound_ringtone_id, show_previews"

// defaultReactionsNotifySettings are the settings of an account that has set
// none: what Telegram answers a fresh account (task 020): reactions to
// messages and stories and poll votes from contacts, the sound
// notificationSoundLocal "default", previews shown.
var defaultReactionsNotifySettings = reactionsNotifySettingsRow{
	MessagesNotifyFrom:  reactionsFromContacts,
	StoriesNotifyFrom:   reactionsFromContacts,
	PollVotesNotifyFrom: reactionsFromContacts,
	SoundKind:           soundKindLocal,
	SoundTitle:          "default",
	SoundData:           "default",
	ShowPreviews:        true,
}

// GetReactionsNotifySettings returns userId's reaction notification
// settings, the defaults while the user has set none.
func (d *Dao) GetReactionsNotifySettings(ctx context.Context, userId int64) (*mtproto.ReactionsNotifySettings, error) {
	var rows []reactionsNotifySettingsRow
	if err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+reactionsNotifySettingsColumns+" FROM user_reactions_notify_settings WHERE user_id = ?", userId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return defaultReactionsNotifySettings.settings()
	}
	return rows[0].settings()
}

// SetReactionsNotifySettings replaces userId's reaction notification
// settings with settings, an absent *_notify_from field meaning nobody, and
// returns them as stored. With keepPollVotes the stored poll votes setting
// stays, and an account that had none gets the default one. Settings that
// fail user.CheckReactionsNotifySettings are SETTINGS_INVALID.
func (d *Dao) SetReactionsNotifySettings(ctx context.Context, userId int64, settings *mtproto.ReactionsNotifySettings, keepPollVotes bool) (*mtproto.ReactionsNotifySettings, error) {
	r, err := reactionsNotifySettingsRowOf(settings)
	if err != nil {
		return nil, err
	}
	update := "messages_notify_from = new.messages_notify_from, stories_notify_from = new.stories_notify_from, "
	if keepPollVotes {
		// Only a new row takes it: the update leaves the stored one.
		r.PollVotesNotifyFrom = defaultReactionsNotifySettings.PollVotesNotifyFrom
	} else {
		update += "poll_votes_notify_from = new.poll_votes_notify_from, "
	}
	update += "sound_kind = new.sound_kind, sound_title = new.sound_title, sound_data = new.sound_data, " +
		"sound_ringtone_id = new.sound_ringtone_id, show_previews = new.show_previews"

	// The answer is read in the transaction of the write: the row as this
	// change left it.
	var rows []reactionsNotifySettingsRow
	err = d.DB.Transact(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec("INSERT INTO user_reactions_notify_settings (user_id, "+reactionsNotifySettingsColumns+
			") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) AS new ON DUPLICATE KEY UPDATE "+update,
			userId, r.MessagesNotifyFrom, r.StoriesNotifyFrom, r.PollVotesNotifyFrom,
			r.SoundKind, r.SoundTitle, r.SoundData, r.SoundRingtoneId, r.ShowPreviews); err != nil {
			return err
		}
		return tx.QueryRowsPartial(&rows,
			"SELECT "+reactionsNotifySettingsColumns+" FROM user_reactions_notify_settings WHERE user_id = ?", userId)
	})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("reactions notify settings of user %d: %d rows after the write", userId, len(rows))
	}
	return rows[0].settings()
}

// reactionsNotifySettingsRowOf is the row of settings; settings it cannot
// keep are SETTINGS_INVALID.
func reactionsNotifySettingsRowOf(settings *mtproto.ReactionsNotifySettings) (reactionsNotifySettingsRow, error) {
	var r reactionsNotifySettingsRow
	err := user.CheckReactionsNotifySettings(settings)
	if err != nil {
		return r, err
	}
	if r.MessagesNotifyFrom, err = reactionsFromCode(settings.GetMessagesNotifyFrom()); err != nil {
		return r, err
	}
	if r.StoriesNotifyFrom, err = reactionsFromCode(settings.GetStoriesNotifyFrom()); err != nil {
		return r, err
	}
	if r.PollVotesNotifyFrom, err = reactionsFromCode(settings.GetPollVotesNotifyFrom()); err != nil {
		return r, err
	}
	sound := settings.GetSound()
	switch sound.GetPredicateName() {
	case mtproto.Predicate_notificationSoundDefault:
		r.SoundKind = soundKindDefault
	case mtproto.Predicate_notificationSoundNone:
		r.SoundKind = soundKindNone
	case mtproto.Predicate_notificationSoundLocal:
		r.SoundKind, r.SoundTitle, r.SoundData = soundKindLocal, sound.GetTitle(), sound.GetData()
	case mtproto.Predicate_notificationSoundRingtone:
		r.SoundKind, r.SoundRingtoneId = soundKindRingtone, sound.GetId()
	default:
		return r, mtproto.ErrSettingsInvalid
	}
	switch settings.GetShowPreviews().GetPredicateName() {
	case mtproto.Predicate_boolTrue:
		r.ShowPreviews = true
	case mtproto.Predicate_boolFalse:
	default:
		return r, mtproto.ErrSettingsInvalid
	}
	return r, nil
}

func reactionsFromCode(from *mtproto.ReactionNotificationsFrom) (int32, error) {
	if from == nil {
		return reactionsFromNobody, nil
	}
	switch from.GetPredicateName() {
	case mtproto.Predicate_reactionNotificationsFromContacts:
		return reactionsFromContacts, nil
	case mtproto.Predicate_reactionNotificationsFromAll:
		return reactionsFromAll, nil
	}
	return 0, mtproto.ErrSettingsInvalid
}

// settings are the reactionsNotifySettings of r. A code it does not know is
// an error: the row was not written by this version.
func (r reactionsNotifySettingsRow) settings() (*mtproto.ReactionsNotifySettings, error) {
	var (
		s   = &mtproto.ReactionsNotifySettings{ShowPreviews: mtproto.ToBool(r.ShowPreviews)}
		err error
	)
	if s.MessagesNotifyFrom, err = reactionsFrom("messages_notify_from", r.MessagesNotifyFrom); err != nil {
		return nil, err
	}
	if s.StoriesNotifyFrom, err = reactionsFrom("stories_notify_from", r.StoriesNotifyFrom); err != nil {
		return nil, err
	}
	if s.PollVotesNotifyFrom, err = reactionsFrom("poll_votes_notify_from", r.PollVotesNotifyFrom); err != nil {
		return nil, err
	}
	switch r.SoundKind {
	case soundKindDefault:
		s.Sound = mtproto.MakeTLNotificationSoundDefault(nil).To_NotificationSound()
	case soundKindNone:
		s.Sound = mtproto.MakeTLNotificationSoundNone(nil).To_NotificationSound()
	case soundKindLocal:
		s.Sound = mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{
			Title: r.SoundTitle,
			Data:  r.SoundData,
		}).To_NotificationSound()
	case soundKindRingtone:
		s.Sound = mtproto.MakeTLNotificationSoundRingtone(&mtproto.NotificationSound{
			Id: r.SoundRingtoneId,
		}).To_NotificationSound()
	default:
		return nil, fmt.Errorf("user_reactions_notify_settings: sound_kind %d", r.SoundKind)
	}
	return mtproto.MakeTLReactionsNotifySettings(s).To_ReactionsNotifySettings(), nil
}

func reactionsFrom(column string, value int32) (*mtproto.ReactionNotificationsFrom, error) {
	switch value {
	case reactionsFromNobody:
		return nil, nil
	case reactionsFromContacts:
		return mtproto.MakeTLReactionNotificationsFromContacts(nil).To_ReactionNotificationsFrom(), nil
	case reactionsFromAll:
		return mtproto.MakeTLReactionNotificationsFromAll(nil).To_ReactionNotificationsFrom(), nil
	}
	return nil, fmt.Errorf("user_reactions_notify_settings: %s %d", column, value)
}
