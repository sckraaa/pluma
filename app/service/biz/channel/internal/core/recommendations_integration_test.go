//go:build integration

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

package core

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// newBroadcast makes a broadcast channel of creator, public unless
// private.
func (f *diffFixture) newBroadcast(creator int64, private bool) int64 {
	f.t.Helper()
	id := f.create(creator)
	f.broadcast(id)
	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)
	if !private {
		f.public(id)
	}
	return id
}

// subscribe puts users in the channel with state (0 in it, 1 left, 2
// banned) and counts those in it.
func (f *diffFixture) subscribe(channelId int64, state int32, users ...int64) {
	f.t.Helper()
	for _, u := range users {
		f.exec("INSERT INTO channel_participants (channel_id, user_id, role, state, `date`) VALUES (?, ?, ?, ?, 1)", channelId, u, channel.RoleMember, state)
	}
	f.exec("UPDATE channels SET participants_count = (SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = 0) WHERE id = ?", channelId, channelId)
}

func (f *diffFixture) recommendations(self, channelId int64) []int64 {
	f.t.Helper()
	r, err := f.c.ChannelGetChannelRecommendations(&channel.TLChannelGetChannelRecommendations{SelfId: self, ChannelId: channelId})
	if err != nil {
		f.t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_chats {
		f.t.Errorf("%s, want messages.chats", r.GetPredicateName())
	}
	ids := make([]int64, 0, len(r.GetChats()))
	for _, c := range r.GetChats() {
		if c.GetPredicateName() != mtproto.Predicate_channel || !c.GetLeft() || !c.GetBroadcast() {
			f.t.Errorf("recommended %s %d: left %v, broadcast %v", c.GetPredicateName(), c.GetId(), c.GetLeft(), c.GetBroadcast())
		}
		ids = append(ids, c.GetId())
	}
	return ids
}

// TestChannelRecommendations: the channels like a channel are the public
// broadcast channels its readers are in, by how many of them, then by size;
// the account's own, left and banned channels, private ones, supergroups,
// deleted and blocked ones are not offered, and bots and deleted accounts
// do not count. For the account itself the channels of the people of its
// channels come first, then the other public channels.
func TestChannelRecommendations(t *testing.T) {
	f := newDiffFixture(t)
	owner, asker := f.user("Owner"), f.user("Asker")
	ann, bob, cid, dan := f.user("Ann"), f.user("Bob"), f.user("Cid"), f.user("Dan")
	bot, gone := f.user("Bot"), f.user("Gone")
	f.exec("UPDATE users SET is_bot = 1 WHERE id = ?", bot)
	f.exec("UPDATE users SET deleted = 1 WHERE id = ?", gone)

	source := f.newBroadcast(owner, false)
	f.subscribe(source, 0, asker, ann, bob, cid, bot, gone)

	two := f.newBroadcast(owner, false) // owner, ann, bob of the source: 3
	f.subscribe(two, 0, ann, bob)
	big := f.newBroadcast(owner, false) // owner, ann: 2, but larger than one
	f.subscribe(big, 0, ann, dan, f.user("E"), f.user("F"))
	one := f.newBroadcast(owner, false) // owner, cid: 2
	f.subscribe(one, 0, cid)
	strangers := f.newBroadcast(f.user("Lone"), false) // no reader of the source
	byBots := f.newBroadcast(f.user("Other"), false)   // a bot and a deleted account only
	f.subscribe(byBots, 0, bot, gone)
	joined := f.newBroadcast(owner, false)
	f.subscribe(joined, 0, ann, asker)
	left := f.newBroadcast(owner, false)
	f.subscribe(left, 0, ann)
	f.subscribe(left, 1, asker)
	banned := f.newBroadcast(owner, false)
	f.subscribe(banned, 0, ann)
	f.subscribe(banned, 2, asker)
	private := f.newBroadcast(owner, true)
	f.subscribe(private, 0, ann)
	group := f.create(owner)
	f.public(group)
	f.subscribe(group, 0, ann)
	deleted := f.newBroadcast(owner, false)
	f.subscribe(deleted, 0, ann)
	f.exec("UPDATE channels SET deleted = 1 WHERE id = ?", deleted)
	blocked := f.newBroadcast(owner, false)
	f.subscribe(blocked, 0, ann)
	f.exec("UPDATE channels SET blocked = 1 WHERE id = ?", blocked)

	want := []int64{two, big, one}
	if got := f.recommendations(asker, source); !slices.Equal(got, want) {
		t.Errorf("like the source: %v, want %v", got, want)
	}

	// Nothing like a channel no one else reads.
	if got := f.recommendations(asker, strangers); len(got) != 0 {
		t.Errorf("like a channel of no readers: %v", got)
	}

	// For the account: the same three first, as its channel is the source
	// and the joined one adds ann alone; then the other public channels by
	// size, among them those of strangers, never its own or those it left.
	got := f.recommendations(asker, 0)
	if len(got) < 3 || !slices.Equal(got[:3], want) {
		t.Fatalf("for the account: %v, want %v first", got, want)
	}
	for _, id := range []int64{source, joined, left, banned, private, group, deleted, blocked} {
		if slices.Contains(got, id) {
			t.Errorf("for the account: %d offered in %v", id, got)
		}
	}
	for _, id := range []int64{strangers, byBots} {
		if !slices.Contains(got[3:], id) {
			t.Errorf("for the account: the public channel %d missing after the others: %v", id, got)
		}
	}
}
