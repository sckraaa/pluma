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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Reaction notification settings (task 020): a fresh account gets what
// Telegram answers one, a set answers the settings as stored, and another
// device of the account reads them. A field the set leaves out is nobody,
// and comes back absent.
func TestReactionsNotifySettings(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")

	contacts := mtproto.MakeTLReactionNotificationsFromContacts(nil).To_ReactionNotificationsFrom()
	all := mtproto.MakeTLReactionNotificationsFromAll(nil).To_ReactionNotificationsFrom()
	defaults := mtproto.MakeTLReactionsNotifySettings(&mtproto.ReactionsNotifySettings{
		MessagesNotifyFrom:  contacts,
		StoriesNotifyFrom:   contacts,
		PollVotesNotifyFrom: contacts,
		Sound:               mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "default", Data: "default"}).To_NotificationSound(),
		ShowPreviews:        mtproto.BoolTrue,
	}).To_ReactionsNotifySettings()
	if got, want := reactionsSettingsText(reactionsNotifySettings(ctx, t, alice)), reactionsSettingsText(defaults); got != want {
		t.Errorf("a fresh account: %s, want %s", got, want)
	}

	// As Android sends them: no stories field, its own "Default" sound.
	sent := mtproto.MakeTLReactionsNotifySettings(&mtproto.ReactionsNotifySettings{
		MessagesNotifyFrom:  all,
		PollVotesNotifyFrom: contacts,
		Sound:               mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: "Default", Data: "Default"}).To_NotificationSound(),
		ShowPreviews:        mtproto.BoolFalse,
	}).To_ReactionsNotifySettings()
	want := reactionsSettingsText(sent)
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountSetReactionsNotifySettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_setReactionsNotifySettings),
		Settings:    sent,
	})
	if err != nil {
		t.Fatalf("account.setReactionsNotifySettings: %v", err)
	}
	if got := reactionsSettingsText(toReactionsNotifySettings(t, o)); got != want {
		t.Errorf("account.setReactionsNotifySettings answered %s, want %s", got, want)
	}

	other := login(ctx, t, phone, "Alice on another device")
	got := reactionsNotifySettings(ctx, t, other)
	if text := reactionsSettingsText(got); text != want {
		t.Errorf("another device: %s, want %s", text, want)
	}
	if got.GetStoriesNotifyFrom() != nil {
		t.Errorf("stories_notify_from left out came back as %s", got.GetStoriesNotifyFrom().GetPredicateName())
	}
}

func reactionsNotifySettings(ctx context.Context, t *testing.T, u *user) *mtproto.ReactionsNotifySettings {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLAccountGetReactionsNotifySettings{
		Constructor: u.c.Constructor(mtproto.Predicate_account_getReactionsNotifySettings),
	})
	if err != nil {
		t.Fatalf("account.getReactionsNotifySettings: %v", err)
	}
	return toReactionsNotifySettings(t, o)
}

func toReactionsNotifySettings(t *testing.T, o mtproto.TLObject) *mtproto.ReactionsNotifySettings {
	t.Helper()
	r, ok := o.(interface {
		To_ReactionsNotifySettings() *mtproto.ReactionsNotifySettings
	})
	if !ok {
		t.Fatalf("returned %T, want reactionsNotifySettings", o)
	}
	return r.To_ReactionsNotifySettings()
}

// reactionsSettingsText writes settings down to compare them; an absent
// *_notify_from field is nobody.
func reactionsSettingsText(s *mtproto.ReactionsNotifySettings) string {
	from := func(f *mtproto.ReactionNotificationsFrom) string {
		if f == nil {
			return "nobody"
		}
		return f.GetPredicateName()
	}
	sound := s.GetSound()
	return fmt.Sprintf("messages %s, stories %s, poll votes %s, %s %q %q %d, previews %s",
		from(s.GetMessagesNotifyFrom()), from(s.GetStoriesNotifyFrom()), from(s.GetPollVotesNotifyFrom()),
		sound.GetPredicateName(), sound.GetTitle(), sound.GetData(), sound.GetId(),
		s.GetShowPreviews().GetPredicateName())
}
