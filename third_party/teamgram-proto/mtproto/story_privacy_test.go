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

package mtproto

import (
	"testing"
	"time"
)

func storyRule(predicate string, users, chats []int64) *PrivacyRule {
	return &PrivacyRule{PredicateName: predicate, Users: users, Chats: chats}
}

// Who a story is for, by its rules: the close friends rule works only for a
// close friend, a user named by the story is let in or kept out before
// anything else, and a story without a rule that lets in is for nobody.
func TestEvaluateStoryPrivacy(t *testing.T) {
	const (
		stranger    = 10
		contact     = 11
		closeFriend = 12
	)
	viewer := func(id int64) StoryViewer {
		return StoryViewer{
			PrivacyViewer: PrivacyViewer{Id: id, Contact: id != stranger},
			CloseFriend:   id == closeFriend,
		}
	}
	all := storyRule(Predicate_privacyValueAllowAll, nil, nil)
	contacts := storyRule(Predicate_privacyValueAllowContacts, nil, nil)
	friends := storyRule(Predicate_privacyValueAllowCloseFriends, nil, nil)
	cases := []struct {
		name  string
		rules []*PrivacyRule
		sees  map[int64]bool
	}{
		{"everyone", []*PrivacyRule{all}, map[int64]bool{stranger: true, contact: true, closeFriend: true}},
		{"everyone but one", []*PrivacyRule{all, storyRule(Predicate_privacyValueDisallowUsers, []int64{contact}, nil)},
			map[int64]bool{stranger: true, contact: false, closeFriend: true}},
		{"contacts", []*PrivacyRule{contacts}, map[int64]bool{stranger: false, contact: true, closeFriend: true}},
		{"contacts but one", []*PrivacyRule{storyRule(Predicate_privacyValueDisallowUsers, []int64{closeFriend}, nil), contacts},
			map[int64]bool{stranger: false, contact: true, closeFriend: false}},
		{"close friends", []*PrivacyRule{friends}, map[int64]bool{stranger: false, contact: false, closeFriend: true}},
		{"close friends but one", []*PrivacyRule{friends, storyRule(Predicate_privacyValueDisallowUsers, []int64{closeFriend}, nil)},
			map[int64]bool{stranger: false, contact: false, closeFriend: false}},
		{"chosen users", []*PrivacyRule{storyRule(Predicate_privacyValueAllowUsers, []int64{stranger}, nil)},
			map[int64]bool{stranger: true, contact: false, closeFriend: false}},
		{"members of a chat, resolved", []*PrivacyRule{storyRule(PredicatePrivacyAllowChatMembers, []int64{contact}, []int64{5})},
			map[int64]bool{stranger: false, contact: true, closeFriend: false}},
		{"members of a chat, not resolved", []*PrivacyRule{storyRule(Predicate_privacyValueAllowChatParticipants, nil, []int64{5})},
			map[int64]bool{stranger: false, contact: false, closeFriend: false}},
		{"everyone but the members of a chat, not resolved", []*PrivacyRule{all, storyRule(Predicate_privacyValueDisallowChatParticipants, nil, []int64{5})},
			map[int64]bool{stranger: false, contact: false, closeFriend: false}},
		{"no rules", nil, map[int64]bool{stranger: false, contact: false, closeFriend: false}},
	}
	for _, tc := range cases {
		for id, want := range tc.sees {
			if got := EvaluateStoryPrivacy(tc.rules, viewer(id)); got != want {
				t.Errorf("%s: viewer %d sees %v, want %v", tc.name, id, got, want)
			}
		}
		// The order of the rules changes nothing.
		reversed := make([]*PrivacyRule, 0, len(tc.rules))
		for i := len(tc.rules) - 1; i >= 0; i-- {
			reversed = append(reversed, tc.rules[i])
		}
		for id, want := range tc.sees {
			if got := EvaluateStoryPrivacy(reversed, viewer(id)); got != want {
				t.Errorf("%s reversed: viewer %d sees %v, want %v", tc.name, id, got, want)
			}
		}
	}
}

// The audience the clients draw on a story: all, then the close friends,
// then the chosen users, then the contacts.
func TestStoryAudienceOf(t *testing.T) {
	all := storyRule(Predicate_privacyValueAllowAll, nil, nil)
	contacts := storyRule(Predicate_privacyValueAllowContacts, nil, nil)
	friends := storyRule(Predicate_privacyValueAllowCloseFriends, nil, nil)
	users := storyRule(Predicate_privacyValueAllowUsers, []int64{1}, nil)
	deny := storyRule(Predicate_privacyValueDisallowUsers, []int64{2}, nil)
	for _, tc := range []struct {
		rules []*PrivacyRule
		want  int
	}{
		{[]*PrivacyRule{all, deny}, StoryAudiencePublic},
		{[]*PrivacyRule{friends}, StoryAudienceCloseFriends},
		{[]*PrivacyRule{users}, StoryAudienceSelected},
		{[]*PrivacyRule{deny, contacts}, StoryAudienceContacts},
		{[]*PrivacyRule{contacts, users}, StoryAudienceContacts},
		{nil, StoryAudienceSelected},
	} {
		if got := StoryAudienceOf(tc.rules); got != tc.want {
			t.Errorf("%v: audience %d, want %d", tc.rules, got, tc.want)
		}
	}
}

// user.stories_max_id is the newest story in the feeds that the viewer may
// open: a story for others and one that has expired do not count, and the
// privacy keys of the user are not read as stories.
func TestStoriesMaxIdFor(t *testing.T) {
	const (
		owner    = 1
		contact  = 2
		stranger = 3
	)
	now := time.Unix(1_000_000, 0)
	later := int32(now.Unix()) + 3600
	earlier := int32(now.Unix()) - 1
	u := MakeTLImmutableUser(&ImmutableUser{
		User:     &UserData{Id: owner},
		Contacts: []*ContactData{{UserId: owner, ContactUserId: contact, CloseFriend: true}},
		KeysPrivacyRules: []*PrivacyKeyRules{
			{Key: PHONE_NUMBER, Rules: []*PrivacyRule{storyRule(Predicate_privacyValueAllowAll, nil, nil)}},
			MakeStoryAudience(1, later, []*PrivacyRule{storyRule(Predicate_privacyValueAllowAll, nil, nil)}),
			MakeStoryAudience(2, later, []*PrivacyRule{storyRule(Predicate_privacyValueAllowContacts, nil, nil)}),
			MakeStoryAudience(3, later, []*PrivacyRule{storyRule(Predicate_privacyValueAllowUsers, []int64{owner}, nil)}),
			MakeStoryAudience(4, earlier, []*PrivacyRule{storyRule(Predicate_privacyValueAllowAll, nil, nil)}),
		},
	}).To_ImmutableUser()
	for viewer, want := range map[int64]int32{owner: 3, contact: 2, stranger: 1} {
		if got := u.StoriesMaxIdFor(viewer, false, now); got != want {
			t.Errorf("viewer %d: stories_max_id %d, want %d", viewer, got, want)
		}
	}
	// Later, when every story has expired, nobody is shown one.
	if got := u.StoriesMaxIdFor(owner, false, now.Add(2*time.Hour)); got != 0 {
		t.Errorf("after the stories expired: stories_max_id %d", got)
	}
}

// A user object built at the real clock: a story in the feeds shows for
// the one it is for and not for another.
func TestUserObjectStoriesMaxId(t *testing.T) {
	later := int32(time.Now().Unix()) + 3600
	owner := MakeTLImmutableUser(&ImmutableUser{
		User:     &UserData{Id: 1},
		Contacts: []*ContactData{{UserId: 1, ContactUserId: 2}},
		KeysPrivacyRules: []*PrivacyKeyRules{
			MakeStoryAudience(7, later, []*PrivacyRule{storyRule(Predicate_privacyValueAllowContacts, nil, nil)}),
		},
	}).To_ImmutableUser()
	contact := MakeTLImmutableUser(&ImmutableUser{User: &UserData{Id: 2}}).To_ImmutableUser()
	stranger := MakeTLImmutableUser(&ImmutableUser{User: &UserData{Id: 3}}).To_ImmutableUser()
	if got := owner.ToUnsafeUser(contact); got.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != 7 || got.GetStoriesMaxId_FLAGINT32().GetValue() != 7 || got.GetStoriesUnavailable() {
		t.Errorf("a contact: %v / %v, want 7", got.GetStoriesMaxId_FLAGRECENTSTORY(), got.GetStoriesMaxId_FLAGINT32())
	}
	// Nothing to open is said, so that a ring the client kept goes out.
	if got := owner.ToUnsafeUser(stranger); got.GetStoriesMaxId_FLAGRECENTSTORY() != nil || got.GetStoriesMaxId_FLAGINT32() != nil || !got.GetStoriesUnavailable() {
		t.Errorf("a stranger: %v / %v, unavailable %v, want none", got.GetStoriesMaxId_FLAGRECENTSTORY(), got.GetStoriesMaxId_FLAGINT32(), got.GetStoriesUnavailable())
	}
	if got := stranger.ToSelfUser(); got.GetStoriesMaxId_FLAGRECENTSTORY() != nil || !got.GetStoriesUnavailable() {
		t.Errorf("an account without stories: %v, unavailable %v", got.GetStoriesMaxId_FLAGRECENTSTORY(), got.GetStoriesUnavailable())
	}
	if got := owner.ToUser(2); got.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != 7 {
		t.Errorf("ToUser for a contact: %v, want 7", got.GetStoriesMaxId_FLAGRECENTSTORY())
	}
	if got := owner.ToSelfUser(); got.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != 7 {
		t.Errorf("the account itself: %v, want 7", got.GetStoriesMaxId_FLAGRECENTSTORY())
	}
}
