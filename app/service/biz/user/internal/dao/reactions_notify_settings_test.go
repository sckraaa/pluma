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
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func fromContacts() *mtproto.ReactionNotificationsFrom {
	return mtproto.MakeTLReactionNotificationsFromContacts(nil).To_ReactionNotificationsFrom()
}

func fromAll() *mtproto.ReactionNotificationsFrom {
	return mtproto.MakeTLReactionNotificationsFromAll(nil).To_ReactionNotificationsFrom()
}

func reactionsSettings(messages, stories, pollVotes *mtproto.ReactionNotificationsFrom, sound *mtproto.NotificationSound, previews bool) *mtproto.ReactionsNotifySettings {
	return mtproto.MakeTLReactionsNotifySettings(&mtproto.ReactionsNotifySettings{
		MessagesNotifyFrom:  messages,
		StoriesNotifyFrom:   stories,
		PollVotesNotifyFrom: pollVotes,
		Sound:               sound,
		ShowPreviews:        mtproto.ToBool(previews),
	}).To_ReactionsNotifySettings()
}

// fromName is the predicate of from, "nobody" for an absent field.
func fromName(from *mtproto.ReactionNotificationsFrom) string {
	if from == nil {
		return "nobody"
	}
	return from.GetPredicateName()
}

// sameSettings tells whether a and b are the same settings, field by field.
func sameSettings(a, b *mtproto.ReactionsNotifySettings) bool {
	return a.GetPredicateName() == b.GetPredicateName() &&
		fromName(a.GetMessagesNotifyFrom()) == fromName(b.GetMessagesNotifyFrom()) &&
		fromName(a.GetStoriesNotifyFrom()) == fromName(b.GetStoriesNotifyFrom()) &&
		fromName(a.GetPollVotesNotifyFrom()) == fromName(b.GetPollVotesNotifyFrom()) &&
		a.GetSound().GetPredicateName() == b.GetSound().GetPredicateName() &&
		a.GetSound().GetTitle() == b.GetSound().GetTitle() &&
		a.GetSound().GetData() == b.GetSound().GetData() &&
		a.GetSound().GetId() == b.GetSound().GetId() &&
		a.GetShowPreviews().GetPredicateName() == b.GetShowPreviews().GetPredicateName()
}

// Every from value and every sound constructor goes to the columns and
// comes back as it was.
func TestReactionsNotifySettingsColumns(t *testing.T) {
	sounds := []*mtproto.NotificationSound{
		mtproto.MakeTLNotificationSoundDefault(nil).To_NotificationSound(),
		mtproto.MakeTLNotificationSoundNone(nil).To_NotificationSound(),
		mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "Default", Data: "Default"}).To_NotificationSound(),
		mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "Звонок", Data: "105.m4a"}).To_NotificationSound(),
		mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{}).To_NotificationSound(),
		mtproto.MakeTLNotificationSoundRingtone(&mtproto.NotificationSound{Id: 5078299559046677200}).To_NotificationSound(),
	}
	froms := []*mtproto.ReactionNotificationsFrom{nil, fromContacts(), fromAll()}
	n := 0
	for _, messages := range froms {
		for _, stories := range froms {
			for _, pollVotes := range froms {
				for _, sound := range sounds {
					n++
					in := reactionsSettings(messages, stories, pollVotes, sound, n%2 == 0)
					r, err := reactionsNotifySettingsRowOf(in)
					if err != nil {
						t.Fatalf("%+v: %v", in, err)
					}
					out, err := r.settings()
					if err != nil {
						t.Fatalf("%+v: %v", r, err)
					}
					if !sameSettings(in, out) {
						t.Errorf("%+v went to %+v and came back as %+v", in, r, out)
					}
				}
			}
		}
	}

	// Each field has its own column.
	r, err := reactionsNotifySettingsRowOf(reactionsSettings(fromAll(), nil, fromContacts(), sounds[0], true))
	if err != nil {
		t.Fatal(err)
	}
	if r.MessagesNotifyFrom != reactionsFromAll || r.StoriesNotifyFrom != reactionsFromNobody || r.PollVotesNotifyFrom != reactionsFromContacts {
		t.Errorf("all, nobody, contacts: the columns %d, %d, %d", r.MessagesNotifyFrom, r.StoriesNotifyFrom, r.PollVotesNotifyFrom)
	}
	r, err = reactionsNotifySettingsRowOf(reactionsSettings(nil, nil, nil, sounds[5], false))
	if err != nil {
		t.Fatal(err)
	}
	if r.SoundKind != soundKindRingtone || r.SoundRingtoneId != 5078299559046677200 || r.SoundTitle != "" || r.SoundData != "" || r.ShowPreviews {
		t.Errorf("a ringtone without previews: %+v", r)
	}
}

// An account that has set nothing gets what Telegram answers a fresh
// account: contacts for all three, the sound notificationSoundLocal
// "default", previews shown.
func TestReactionsNotifySettingsDefaults(t *testing.T) {
	s, err := defaultReactionsNotifySettings.settings()
	if err != nil {
		t.Fatal(err)
	}
	want := reactionsSettings(fromContacts(), fromContacts(), fromContacts(),
		mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "default", Data: "default"}).To_NotificationSound(), true)
	if !sameSettings(s, want) {
		t.Errorf("the defaults %+v, want %+v", s, want)
	}
}

// Settings the columns cannot keep are SETTINGS_INVALID, and a stored code
// this version does not know is an error, never a guess.
func TestReactionsNotifySettingsInvalid(t *testing.T) {
	sound := mtproto.MakeTLNotificationSoundDefault(nil).To_NotificationSound()
	for name, s := range map[string]*mtproto.ReactionsNotifySettings{
		"no settings":     nil,
		"no sound":        reactionsSettings(nil, nil, nil, nil, true),
		"an unknown from": reactionsSettings(&mtproto.ReactionNotificationsFrom{Constructor: 1}, nil, nil, sound, true),
		"a long title": reactionsSettings(nil, nil, nil,
			mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: strings.Repeat("я", 256)}).To_NotificationSound(), true),
	} {
		if _, err := reactionsNotifySettingsRowOf(s); !errors.Is(err, mtproto.ErrSettingsInvalid) {
			t.Errorf("%s: %v, want SETTINGS_INVALID", name, err)
		}
	}

	for name, r := range map[string]reactionsNotifySettingsRow{
		"messages_notify_from":   {MessagesNotifyFrom: 3},
		"stories_notify_from":    {StoriesNotifyFrom: -1},
		"poll_votes_notify_from": {PollVotesNotifyFrom: 9},
		"sound_kind":             {SoundKind: 4},
	} {
		if s, err := r.settings(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("an unknown %s: %v, %v", name, s, err)
		}
	}
}
