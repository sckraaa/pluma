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

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func sublistMessage(id int32, peer int64) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id:          id,
		PeerId:      mtproto.MakePeerChannel(100),
		SavedPeerId: mtproto.MakePeerUser(peer),
		Message:     "text",
	}).To_Message()
}

// A subscriber gets the updates of its own sublist alone: the messages by
// their saved_peer_id, the ids of a deletion and of a pin by the sublists
// they are in, and nothing of any other kind.
func TestSublistView(t *testing.T) {
	const alice, bob = int64(7), int64(8)
	updates := func() []*mtproto.Update {
		return []*mtproto.Update{
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: sublistMessage(1, alice), Pts_INT32: 2, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: sublistMessage(2, bob), Pts_INT32: 3, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: sublistMessage(2, bob), Pts_INT32: 4, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{ChannelId: 100, Messages: []int32{1, 2, 3}, Pts_INT32: 7, PtsCount: 3}).To_Update(),
			mtproto.MakeTLUpdatePinnedChannelMessages(&mtproto.Update{ChannelId: 100, Messages: []int32{2}, Pinned: true, Pts_INT32: 8, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{Peer_PEER: mtproto.MakePeerChannel(100), MsgId_INT32: 3}).To_Update(),
			mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: 100}).To_Update(),
			mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: 2, RandomId: 5}).To_Update(),
		}
	}
	if got := NamedIds(updates()); !slices.Equal(got, []int32{1, 2, 3}) {
		t.Fatalf("named ids %v, want 1 2 3", got)
	}
	sublists := map[int32]int64{1: alice, 2: bob, 3: alice}

	got := SublistView(updates(), alice, sublists)
	if len(got) != 3 {
		t.Fatalf("alice gets %d updates, want her message, the deletion and the reaction: %v", len(got), got)
	}
	if got[0].GetMessage_MESSAGE().GetId() != 1 {
		t.Errorf("the first update is %v, want her message 1", got[0])
	}
	if d := got[1]; d.GetPredicateName() != mtproto.Predicate_updateDeleteChannelMessages || !slices.Equal(d.GetMessages(), []int32{1, 3}) || d.GetPts_INT32() != 7 || d.GetPtsCount() != 3 {
		t.Errorf("the deletion is %v, want her ids 1 and 3 with the pts of the journal", d)
	}
	if got[2].GetPredicateName() != mtproto.Predicate_updateMessageReactions {
		t.Errorf("the last update is %v, want the reaction to her message", got[2])
	}

	got = SublistView(updates(), bob, sublists)
	var kinds []string
	for _, u := range got {
		kinds = append(kinds, u.GetPredicateName())
	}
	want := []string{mtproto.Predicate_updateNewChannelMessage, mtproto.Predicate_updateEditChannelMessage, mtproto.Predicate_updateDeleteChannelMessages, mtproto.Predicate_updatePinnedChannelMessages}
	if !slices.Equal(kinds, want) {
		t.Fatalf("bob gets %v, want %v", kinds, want)
	}
	if !slices.Equal(got[2].GetMessages(), []int32{2}) {
		t.Errorf("bob's deletion names %v, want 2", got[2].GetMessages())
	}

	if got = SublistView(updates(), 9, sublists); len(got) != 0 {
		t.Errorf("one with no sublist gets %v", got)
	}
}

// A monoforum is the supergroup of a channel's direct messages: never left
// for one who may open it, forbidden for anyone else; it names its channel
// only while the direct messages are on, and so does the channel.
func TestMonoforumChat(t *testing.T) {
	mono := &ImmutableChannel{Id: 200, AccessHash: 9, Title: "news", Megagroup: true, Monoforum: true, LinkedMonoforumId: 100, BroadcastMessagesAllowed: true, Date: viewCreated}
	member := &ImmutableChannelParticipant{ChannelId: 200, UserId: viewer, State: StateIn, Date: viewCreated}
	admin := &ImmutableChannelParticipant{ChannelId: 200, UserId: viewer, State: StateIn, Role: RoleAdmin, AdminRights: mtproto.ADMIN_MANAGE_DIRECT_MESSAGES | mtproto.ADMIN_DELETE_MESSAGES, Date: viewCreated}

	c := mono.ToChat(member, viewNow)
	if c.GetPredicateName() != mtproto.Predicate_channel || !c.GetMonoforum() || !c.GetMegagroup() || c.GetLeft() || c.GetAdminRights() != nil {
		t.Errorf("a subscriber's monoforum: %v", c)
	}
	// A subscriber is not offered to pin, to change the info or to invite.
	if r := c.GetDefaultBannedRights(); !r.GetPinMessages() || !r.GetChangeInfo() || !r.GetInviteUsers() || r.GetSendMessages() || r.GetSendMedia() {
		t.Errorf("the default banned rights of a monoforum: %v", r)
	}
	if c.GetLinkedMonoforumId().GetValue() != 100 || c.GetBroadcastMessagesAllowed() {
		t.Errorf("the monoforum names %v, allowed %v; want its channel 100 and no flag of a channel", c.GetLinkedMonoforumId(), c.GetBroadcastMessagesAllowed())
	}
	if c = mono.ToChat(admin, viewNow); !c.GetAdminRights().GetManageDirectMessages() || !c.GetAdminRights().GetDeleteMessages() || c.GetCreator() {
		t.Errorf("an admin's monoforum: %v", c)
	}
	if c = mono.ToChat(nil, viewNow); c.GetPredicateName() != mtproto.Predicate_channelForbidden || !c.GetMonoforum() {
		t.Errorf("the monoforum of one without access: %v", c)
	}

	off := &ImmutableChannel{Id: 200, AccessHash: 9, Title: "news", Megagroup: true, Monoforum: true, LinkedMonoforumId: 100, Date: viewCreated}
	if c = off.ToChat(member, viewNow); c.GetLinkedMonoforumId() != nil || !c.GetMonoforum() {
		t.Errorf("a monoforum switched off: %v", c)
	}

	broadcast := &ImmutableChannel{Id: 100, AccessHash: 5, Title: "news", Broadcast: true, Username: "news", LinkedMonoforumId: 200, BroadcastMessagesAllowed: true, Date: viewCreated}
	if c = broadcast.ToChat(nil, viewNow); !c.GetBroadcastMessagesAllowed() || c.GetLinkedMonoforumId().GetValue() != 200 || c.GetMonoforum() {
		t.Errorf("the channel with direct messages: %v", c)
	}
	broadcast.BroadcastMessagesAllowed = false
	if c = broadcast.ToChat(nil, viewNow); c.GetBroadcastMessagesAllowed() || c.GetLinkedMonoforumId() != nil {
		t.Errorf("the channel with direct messages off: %v", c)
	}
}
