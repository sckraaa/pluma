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
	"time"
)

// pluma: who sees a story (task 084, ADR 0054).
//
// The stories a user has in the feeds travel with the user the way the rules
// of its privacy keys do (ImmutableUser.KeysPrivacyRules, ADR 0031): one
// entry per story under a key of its own, holding the rules of the story and
// the moment it expires. Every service that shows the user reads
// user.stories_max_id for a viewer from them, so the ring of a story is
// drawn only for those the story is for. The entries live in the views of
// the user service and never reach a client or the privacy settings.

// storyAudienceKeyBase is where the keys of the stories start: above every
// privacy key of the schema.
const storyAudienceKeyBase = 1 << 20

// PredicateStoryExpire is the rule that carries the expire date of a story
// in its audience, in Users.
const PredicateStoryExpire = "pluma_storyExpire"

// MakeStoryAudience is the entry of the story id that expires at expireDate
// and is shown by rules.
func MakeStoryAudience(id, expireDate int32, rules []*PrivacyRule) *PrivacyKeyRules {
	out := make([]*PrivacyRule, 0, len(rules)+1)
	out = append(out, &PrivacyRule{PredicateName: PredicateStoryExpire, Users: []int64{int64(expireDate)}})
	out = append(out, rules...)
	return MakeTLPrivacyKeyRules(&PrivacyKeyRules{
		Key:   storyAudienceKeyBase + id,
		Rules: out,
	}).To_PrivacyKeyRules()
}

// IsStoryAudience tells an entry of a story from the rules of a privacy key.
func IsStoryAudience(k *PrivacyKeyRules) bool {
	return k.GetKey() > storyAudienceKeyBase
}

// storyAudience reads an entry back: the id of the story and when it
// expires.
func storyAudience(k *PrivacyKeyRules) (id, expireDate int32) {
	id = k.GetKey() - storyAudienceKeyBase
	for _, r := range k.GetRules() {
		if r.GetPredicateName() == PredicateStoryExpire && len(r.GetUsers()) == 1 {
			expireDate = int32(r.GetUsers()[0])
		}
	}
	return
}

// StoryViewer is what the rules of a story need to know of the user who
// looks at it.
type StoryViewer struct {
	PrivacyViewer
	// CloseFriend says the owner of the story has the viewer among its
	// close friends.
	CloseFriend bool
}

// The audience of a story, the way the clients show it: the flags public,
// close_friends, contacts and selected_contacts of storyItem.
const (
	StoryAudienceSelected = iota
	StoryAudienceCloseFriends
	StoryAudienceContacts
	StoryAudiencePublic
)

// StoryAudienceOf is the audience the rules of a story name, read as the
// clients read them (Android StoryPrivacyBottomSheet.java:4035-4096: all,
// then close friends, then the chosen users, then the contacts).
func StoryAudienceOf(rules []*PrivacyRule) int {
	var all, friends, users, contacts bool
	for _, r := range rules {
		switch r.GetPredicateName() {
		case Predicate_privacyValueAllowAll:
			all = true
		case Predicate_privacyValueAllowCloseFriends:
			friends = true
		case Predicate_privacyValueAllowUsers, Predicate_privacyValueAllowChatParticipants, PredicatePrivacyAllowChatMembers:
			users = true
		case Predicate_privacyValueAllowContacts:
			contacts = true
		}
	}
	switch {
	case all:
		return StoryAudiencePublic
	case friends:
		return StoryAudienceCloseFriends
	case users && !contacts:
		return StoryAudienceSelected
	case contacts:
		return StoryAudienceContacts
	}
	return StoryAudienceSelected
}

// EvaluateStoryPrivacy answers whether the rules of a story show it to v.
// It is EvaluatePrivacy with the rule of the close friends, which only
// stories have: a user the story names is let in or kept out first, then
// the chats, then the close friends, then the base rule. A story without a
// base rule is for those it names only.
func EvaluateStoryPrivacy(rules []*PrivacyRule, v StoryViewer) bool {
	var friends bool
	for _, r := range rules {
		if r.GetPredicateName() == Predicate_privacyValueAllowCloseFriends {
			friends = true
		}
	}
	if !friends || !v.CloseFriend {
		return EvaluatePrivacy(rules, v.PrivacyViewer)
	}
	// A close friend the story keeps out by name or by chat stays out.
	denied := make([]*PrivacyRule, 0, len(rules)+1)
	for _, r := range rules {
		switch r.GetPredicateName() {
		case Predicate_privacyValueDisallowUsers, Predicate_privacyValueDisallowChatParticipants, PredicatePrivacyDisallowChatMembers:
			denied = append(denied, r)
		}
	}
	denied = append(denied, &PrivacyRule{PredicateName: Predicate_privacyValueAllowAll})
	return EvaluatePrivacy(denied, v.PrivacyViewer)
}

// StoryViewerOf is viewerId as the stories of m see it, from the rows of
// m's contacts loaded for the viewer. Without them the viewer reads as a
// stranger, which hides rather than shows.
func (m *ImmutableUser) StoryViewerOf(viewerId int64, bot bool) StoryViewer {
	row := m.GetContactData(viewerId)
	return StoryViewer{
		PrivacyViewer: PrivacyViewer{Id: viewerId, Contact: row != nil, Premium: !bot, Bot: bot},
		CloseFriend:   row.GetCloseFriend(),
	}
}

// StoriesMaxIdFor is user.stories_max_id of m for viewerId at now: the
// newest story of m in the feeds that the viewer may open, 0 for none. The
// account sees all of its own.
func (m *ImmutableUser) StoriesMaxIdFor(viewerId int64, bot bool, now time.Time) int32 {
	var (
		max    int32
		viewer = m.StoryViewerOf(viewerId, bot)
		unix   = int32(now.Unix())
	)
	for _, k := range m.GetKeysPrivacyRules() {
		if !IsStoryAudience(k) {
			continue
		}
		id, expire := storyAudience(k)
		if id <= max || expire <= unix {
			continue
		}
		if m.Id() == viewerId || EvaluateStoryPrivacy(k.GetRules(), viewer) {
			max = id
		}
	}
	return max
}

// storiesMaxIdOf fills user.stories_max_id for viewerId in its two forms:
// the int of the older layers and recentStory, and stories_unavailable when
// the viewer has no story of the user to open. The flag is what clears a
// ring a client still keeps: without max_id and without the flag, iOS
// keeps the story id it knew (TelegramCore UpdatePeers.swift:153-166) and
// Desktop leaves its state as it was (data/data_session.cpp:739-744), so
// an expired or deleted story would keep its ring. Telegram sets the flag
// on an account without stories (test/conformance/testdata/
// saved_messages.json, the self user).
func (m *ImmutableUser) storiesMaxIdOf(user *User, viewerId int64, bot bool) {
	if id := m.StoriesMaxIdFor(viewerId, bot, time.Now()); id > 0 {
		user.StoriesMaxId_FLAGINT32 = MakeFlagsInt32(id)
		user.StoriesMaxId_FLAGRECENTSTORY = MakeTLRecentStory(&RecentStory{MaxId: MakeFlagsInt32(id)}).To_RecentStory()
		user.StoriesUnavailable = false
	} else {
		user.StoriesMaxId_FLAGINT32 = nil
		user.StoriesMaxId_FLAGRECENTSTORY = nil
		user.StoriesUnavailable = true
	}
}
