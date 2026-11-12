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

package stories

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func allow(predicate string, users ...int64) *mtproto.PrivacyRule {
	return &mtproto.PrivacyRule{PredicateName: predicate, Users: users}
}

// storyItem as each viewer sees it: the audience is drawn for everyone, who
// the story is for and who opened it only for its owner.
func TestItem(t *testing.T) {
	st := &Story{
		Peer: UserPeer(1), Id: 4, Date: 100, ExpireDate: 200, Pinned: true, Noforwards: true, Edited: true,
		Privacy:       []*mtproto.PrivacyRule{allow(mtproto.Predicate_privacyValueAllowCloseFriends)},
		Caption:       "hello",
		ForwardsCount: 2,
	}
	stats := &Stats{Views: 3, Reactions: 2, ByKind: []ReactionCount{{Reaction: "e❤", Count: 2}}, Recent: []int64{5, 6, 7}}

	own := Item(st, Seen{Out: true, Owner: true, Stats: stats})
	if own.GetPredicateName() != mtproto.Predicate_storyItem || !own.GetOut() || !own.GetCloseFriends() || own.GetPublic() || own.GetContacts() || own.GetSelectedContacts() {
		t.Errorf("the owner's flags: %v", own)
	}
	if !own.GetPinned() || !own.GetNoforwards() || !own.GetEdited() || own.GetCaption().GetValue() != "hello" || len(own.GetPrivacy()) != 1 {
		t.Errorf("the owner's story: %v", own)
	}
	v := own.GetViews()
	if v.GetViewsCount() != 3 || v.GetReactionsCount_FLAGINT32().GetValue() != 2 || v.GetForwardsCount().GetValue() != 2 || !v.GetHasViewers() || len(v.GetRecentViewers()) != 3 || len(v.GetReactions()) != 1 {
		t.Errorf("the owner's views: %v", v)
	}

	other := Item(st, Seen{SentReaction: "e❤"})
	if other.GetOut() || !other.GetCloseFriends() || other.GetPrivacy() != nil || other.GetViews() != nil {
		t.Errorf("a viewer's story: %v", other)
	}
	if other.GetSentReaction().GetEmoticon() != "❤" {
		t.Errorf("a viewer's reaction: %v", other.GetSentReaction())
	}
}

// The audience flag of each kind of rules, and a channel's story, which is
// for everyone who sees the channel.
func TestItemAudience(t *testing.T) {
	for name, tc := range map[string]struct {
		rules []*mtproto.PrivacyRule
		flag  func(*mtproto.StoryItem) bool
	}{
		"everyone":      {[]*mtproto.PrivacyRule{allow(mtproto.Predicate_privacyValueAllowAll)}, (*mtproto.StoryItem).GetPublic},
		"contacts":      {[]*mtproto.PrivacyRule{allow(mtproto.Predicate_privacyValueAllowContacts)}, (*mtproto.StoryItem).GetContacts},
		"close friends": {[]*mtproto.PrivacyRule{allow(mtproto.Predicate_privacyValueAllowCloseFriends)}, (*mtproto.StoryItem).GetCloseFriends},
		"chosen":        {[]*mtproto.PrivacyRule{allow(mtproto.Predicate_privacyValueAllowUsers, 9)}, (*mtproto.StoryItem).GetSelectedContacts},
	} {
		item := Item(&Story{Peer: UserPeer(1), Id: 1, Privacy: tc.rules}, Seen{})
		n := 0
		for _, set := range []bool{item.GetPublic(), item.GetContacts(), item.GetCloseFriends(), item.GetSelectedContacts()} {
			if set {
				n++
			}
		}
		if !tc.flag(item) || n != 1 {
			t.Errorf("%s: %v", name, item)
		}
	}
	ch := Item(&Story{Peer: ChannelPeer(8), Id: 1, SenderId: 3}, Seen{Stats: &Stats{Views: 10, Recent: []int64{4}}})
	if !ch.GetPublic() || ch.GetPrivacy() != nil {
		t.Errorf("a channel's story: %v", ch)
	}
	// Its counters are everyone's, its viewers nobody's.
	if v := ch.GetViews(); v.GetViewsCount() != 10 || v.GetHasViewers() || len(v.GetRecentViewers()) != 0 {
		t.Errorf("a channel's views: %v", v)
	}
}

func TestPeerStoriesAndDeleted(t *testing.T) {
	list := []*Story{{Peer: UserPeer(1), Id: 1}, {Peer: UserPeer(1), Id: 2}}
	ps := PeerStories(UserPeer(1), list, 1, func(*Story) Seen { return Seen{} })
	if ps.GetPeer().GetUserId() != 1 || ps.GetMaxReadId().GetValue() != 1 || len(ps.GetStories()) != 2 {
		t.Errorf("peerStories: %v", ps)
	}
	if ps = PeerStories(ChannelPeer(3), nil, 0, func(*Story) Seen { return Seen{} }); ps.GetPeer().GetChannelId() != 3 || ps.GetMaxReadId() != nil {
		t.Errorf("peerStories of a channel never read: %v", ps)
	}
	if d := Deleted(9); d.GetPredicateName() != mtproto.Predicate_storyItemDeleted || d.GetId() != 9 {
		t.Errorf("deleted: %v", d)
	}
}

func TestValidPeriod(t *testing.T) {
	for _, p := range []int32{21600, 43200, 86400, 172800} {
		if !ValidPeriod(p) {
			t.Errorf("period %d refused", p)
		}
	}
	for _, p := range []int32{0, 1, 3600, 86401, 604800} {
		if ValidPeriod(p) {
			t.Errorf("period %d accepted", p)
		}
	}
}
