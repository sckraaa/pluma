//go:build e2e

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

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The history of a supergroup deleted for oneself, for everyone and by one
// author (task 043): a member hides its history from itself alone (the update
// in the answer, channelFull.available_min_id, the history and the
// difference); the creator deletes a member's messages a portion at a time
// and all the history for everyone; a member may do neither.
func TestChannelDeleteHistory(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "clear", true)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	first := sendMessageID(ctx, t, alice, peer(alice), "alice's first", randomID())
	last := sendMessageID(ctx, t, bob, peer(bob), "bob's first", randomID())

	deleteHistory := func(u *user, forEveryone bool, maxId int32) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsDeleteHistory9BAA9647{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_deleteHistory9BAA9647),
			ForEveryone: forEveryone,
			Channel:     input(u),
			MaxId:       maxId,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	history := func(u *user) map[int32]bool {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        peer(u),
			Limit:       50,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		out := map[int32]bool{}
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			out[m.GetId()] = true
		}
		return out
	}

	// Bob clears it for himself; Alice keeps it.
	if _, err := deleteHistory(bob, true, last); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member deleting for everyone: %v", err)
	}
	r, err := deleteHistory(bob, false, last)
	if err != nil {
		t.Fatalf("channels.deleteHistory for oneself: %v", err)
	}
	if u := r.GetUpdates(); len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateChannelAvailableMessages || u[0].GetAvailableMinId() != last {
		t.Fatalf("the answer: %v", r)
	}
	if h := history(bob); h[first] || h[last] {
		t.Errorf("the hidden history is still shown to Bob: %v", h)
	}
	if h := history(alice); !h[first] || !h[last] {
		t.Errorf("Alice's history changed: %v", h)
	}
	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input(bob),
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	if got := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetAvailableMinId().GetValue(); got != last {
		t.Errorf("channelFull.available_min_id %d, want %d", got, last)
	}

	// Bob writes again, Alice takes his messages away.
	bobs := make([]int32, 0, 3)
	for i := 0; i < 3; i++ {
		bobs = append(bobs, sendMessageID(ctx, t, bob, peer(bob), "bob's again", randomID()))
	}
	deleteParticipant := func(u *user, of *user) (*mtproto.Messages_AffectedHistory, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsDeleteParticipantHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_deleteParticipantHistory),
			Channel:     input(u),
			Participant: peerOf(ctx, t, u, of),
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
		}).To_Messages_AffectedHistory(), nil
	}
	if _, err = deleteParticipant(bob, alice); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member deleting another's: %v", err)
	}
	affected, err := deleteParticipant(alice, bob)
	if err != nil {
		t.Fatalf("channels.deleteParticipantHistory: %v", err)
	}
	if affected.GetPtsCount() != 5 || affected.GetOffset() != 0 { // with the message of his joining
		t.Errorf("the answer: %v", affected)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateDeleteChannelMessages, func(u *mtproto.Update) bool {
		return u.GetChannelId() == ch.GetId() && len(u.GetMessages()) == 5 && u.GetPts_INT32() == affected.GetPts()
	})
	h := history(alice)
	for _, id := range append(bobs, last) {
		if h[id] {
			t.Errorf("Bob's message %d is still there", id)
		}
	}

	// All of it for everyone, up to and beyond the last message.
	r, err = deleteHistory(alice, true, 1<<31-1)
	if err != nil {
		t.Fatalf("channels.deleteHistory for everyone: %v", err)
	}
	if u := r.GetUpdates(); len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateDeleteChannelMessages {
		t.Fatalf("the answer: %v", r)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateDeleteChannelMessages, func(u *mtproto.Update) bool {
		return u.GetChannelId() == ch.GetId() && u.GetPts_INT32() == r.GetUpdates()[0].GetPts_INT32()
	})
	if h := history(alice); h[first] {
		t.Errorf("Alice's message is still there after a clear for everyone: %v", h)
	}
}
