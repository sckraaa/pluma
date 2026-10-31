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

//go:build integration

package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// reactionsFromName is the setting of a *_notify_from field: "nobody" when
// absent.
func reactionsFromName(from *mtproto.ReactionNotificationsFrom) string {
	switch {
	case from == nil:
		return "nobody"
	case from.GetPredicateName() == mtproto.Predicate_reactionNotificationsFromContacts:
		return "contacts"
	case from.GetPredicateName() == mtproto.Predicate_reactionNotificationsFromAll:
		return "all"
	}
	return "?" + from.GetPredicateName()
}

// reactionsSettingsText writes settings down to compare them.
func reactionsSettingsText(s *mtproto.ReactionsNotifySettings) string {
	sound := s.GetSound()
	return fmt.Sprintf("%s messages %s, stories %s, poll votes %s, %s %q %q %d, previews %s",
		s.GetPredicateName(),
		reactionsFromName(s.GetMessagesNotifyFrom()),
		reactionsFromName(s.GetStoriesNotifyFrom()),
		reactionsFromName(s.GetPollVotesNotifyFrom()),
		sound.GetPredicateName(), sound.GetTitle(), sound.GetData(), sound.GetId(),
		s.GetShowPreviews().GetPredicateName())
}

func newReactionsSettings(messages, stories, pollVotes *mtproto.ReactionNotificationsFrom, sound *mtproto.NotificationSound, previews bool) *mtproto.ReactionsNotifySettings {
	return mtproto.MakeTLReactionsNotifySettings(&mtproto.ReactionsNotifySettings{
		MessagesNotifyFrom:  messages,
		StoriesNotifyFrom:   stories,
		PollVotesNotifyFrom: pollVotes,
		Sound:               sound,
		ShowPreviews:        mtproto.ToBool(previews),
	}).To_ReactionsNotifySettings()
}

// Reaction notification settings (task 020): an account that has set none
// has Telegram's defaults; a set replaces them as a whole, an absent field
// being nobody, keeps the sound as sent and answers the settings as stored;
// with keep_poll_votes the stored poll votes setting stays. Each account
// has its own.
func TestReactionsNotifySettings(t *testing.T) {
	c := userCore(t)
	anna, _ := newUser(t, "Anna")
	boris, _ := newUser(t, "Boris")
	contacts := mtproto.MakeTLReactionNotificationsFromContacts(nil).To_ReactionNotificationsFrom()
	all := mtproto.MakeTLReactionNotificationsFromAll(nil).To_ReactionNotificationsFrom()
	defaultSound := mtproto.MakeTLNotificationSoundDefault(nil).To_NotificationSound()
	defaults := newReactionsSettings(contacts, contacts, contacts,
		mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "default", Data: "default"}).To_NotificationSound(), true)

	get := func(u int64) string {
		t.Helper()
		r, err := c.UserGetReactionsNotifySettings(&user.TLUserGetReactionsNotifySettings{UserId: u})
		if err != nil {
			t.Fatalf("get of user %d: %v", u, err)
		}
		return reactionsSettingsText(r)
	}
	set := func(u int64, s *mtproto.ReactionsNotifySettings, keepPollVotes bool) string {
		t.Helper()
		r, err := c.UserSetReactionsNotifySettings(&user.TLUserSetReactionsNotifySettings{
			UserId:        u,
			Settings:      s,
			KeepPollVotes: mtproto.ToBool(keepPollVotes),
		})
		if err != nil {
			t.Fatalf("set of user %d: %v", u, err)
		}
		return reactionsSettingsText(r)
	}

	if got, want := get(anna), reactionsSettingsText(defaults); got != want {
		t.Fatalf("no settings: %s, want %s", got, want)
	}

	// Every sound and every from, as sent; absent fields stay absent.
	for i, s := range []*mtproto.ReactionsNotifySettings{
		newReactionsSettings(all, nil, contacts, defaultSound, false),
		newReactionsSettings(nil, all, nil, mtproto.MakeTLNotificationSoundNone(nil).To_NotificationSound(), true),
		newReactionsSettings(contacts, contacts, all,
			mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "Default", Data: "Default"}).To_NotificationSound(), false),
		newReactionsSettings(nil, nil, nil,
			mtproto.MakeTLNotificationSoundRingtone(&mtproto.NotificationSound{Id: 5078299559046677200}).To_NotificationSound(), true),
	} {
		want := reactionsSettingsText(s)
		if got := set(anna, s, false); got != want {
			t.Errorf("set %d answered %s, want %s", i, got, want)
		}
		if got := get(anna); got != want {
			t.Errorf("after set %d: %s, want %s", i, got, want)
		}
	}
	if n := count(t, "SELECT COUNT(*) FROM user_reactions_notify_settings WHERE user_id = ?", anna); n != 1 {
		t.Errorf("%d rows of the settings of one user", n)
	}

	// keep_poll_votes leaves the stored poll votes setting, here all.
	set(anna, newReactionsSettings(nil, nil, all, defaultSound, true), false)
	sent := newReactionsSettings(contacts, nil, nil, defaultSound, false)
	want := reactionsSettingsText(newReactionsSettings(contacts, nil, all, defaultSound, false))
	if got := set(anna, sent, true); got != want {
		t.Errorf("a set keeping poll votes answered %s, want %s", got, want)
	}
	if got := get(anna); got != want {
		t.Errorf("after a set keeping poll votes: %s, want %s", got, want)
	}
	// The same without keep_poll_votes: absent is nobody.
	if got, want := set(anna, sent, false), reactionsSettingsText(sent); got != want {
		t.Errorf("a set of poll votes absent answered %s, want %s", got, want)
	}

	// Boris has set nothing: his settings are the defaults, whatever Anna
	// did, and keeping poll votes on the first set takes the default one.
	if got, want := get(boris), reactionsSettingsText(defaults); got != want {
		t.Errorf("another user: %s, want %s", got, want)
	}
	want = reactionsSettingsText(newReactionsSettings(all, all, contacts, defaultSound, true))
	if got := set(boris, newReactionsSettings(all, all, nil, defaultSound, true), true); got != want {
		t.Errorf("a first set keeping poll votes answered %s, want %s", got, want)
	}
	if got, want := get(anna), reactionsSettingsText(sent); got != want {
		t.Errorf("Anna after Boris's set: %s, want %s", got, want)
	}

	// Nothing is stored for no user or settings the server cannot keep.
	if _, err := c.UserSetReactionsNotifySettings(&user.TLUserSetReactionsNotifySettings{
		Settings: defaults, KeepPollVotes: mtproto.BoolFalse,
	}); err == nil {
		t.Error("a set for the user 0 succeeded")
	}
	if _, err := c.UserSetReactionsNotifySettings(&user.TLUserSetReactionsNotifySettings{
		UserId: anna, Settings: newReactionsSettings(all, all, all, nil, true), KeepPollVotes: mtproto.BoolFalse,
	}); !errors.Is(err, mtproto.ErrSettingsInvalid) {
		t.Errorf("a set without a sound: %v, want SETTINGS_INVALID", err)
	}
	if got, want := get(anna), reactionsSettingsText(sent); got != want {
		t.Errorf("after the refused sets: %s, want %s", got, want)
	}
}
