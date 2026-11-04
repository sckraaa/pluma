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

// TestChannelRejoinAndDelete: the owner's report (task 055) end to end. Bob
// joins a private channel by its link, leaves, and joins again by the same
// link; Alice, its creator, deletes it; Bob's devices get updateChannel with
// the channel as channelForbidden, its difference is CHANNEL_PRIVATE and it
// is gone from his dialogs.
func TestChannelRejoinAndDelete(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "delete", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	link := channelInviteHash(ctx, t, alice, input(alice))
	join := func() {
		t.Helper()
		if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        link,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	join()
	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsLeaveChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_leaveChannel),
		Channel:     input(bob),
	}); err != nil {
		t.Fatalf("channels.leaveChannel: %v", err)
	}
	// The leave tells Bob's devices too, with the channel forbidden to him
	// now: taken off the queue, so that only the deletion's can match below.
	if _, err := bob.c.WaitUpdate(ctx, forbiddenChannel(ch.GetId())); err != nil {
		t.Fatalf("Bob's update of his leave: %v", err)
	}
	join() // the same link again
	if inv := checkInvite(ctx, t, bob, link); inv.GetPredicateName() != mtproto.Predicate_chatInviteAlready {
		t.Errorf("the link to a member again: %s", inv.GetPredicateName())
	}

	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsDeleteChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_deleteChannel),
		Channel:     input(bob),
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member deletes: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsDeleteChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_deleteChannel),
		Channel:     input(alice),
	})
	if err != nil {
		t.Fatalf("channels.deleteChannel: %v", err)
	}
	if chats := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats(); len(chats) != 1 || chats[0].GetPredicateName() != mtproto.Predicate_channelForbidden {
		t.Errorf("the creator's answer: %v", chats)
	}

	if _, err = bob.c.WaitUpdate(ctx, forbiddenChannel(ch.GetId())); err != nil {
		t.Fatalf("Bob's updateChannel with channelForbidden: %v", err)
	}
	// Gone from Bob's dialogs, as from Alice's.
	for _, u := range []*user{bob, alice} {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getDialogs),
			OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
			Limit:       100,
		})
		if err != nil {
			t.Fatalf("messages.getDialogs: %v", err)
		}
		for _, d := range o.(interface {
			To_Messages_Dialogs() *mtproto.Messages_Dialogs
		}).To_Messages_Dialogs().GetDialogs() {
			if d.GetPeer().GetChannelId() == ch.GetId() {
				t.Errorf("the deleted channel is in the dialogs of %d", u.self.GetId())
			}
		}
	}
	if _, err = channelDifference(ctx, bob, ch, hash, 1, 10); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
		t.Errorf("Bob's difference of a deleted channel: %v, want CHANNEL_PRIVATE", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsDeleteChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_deleteChannel),
		Channel:     input(alice),
	}); err == nil {
		t.Error("a deleted channel deleted again")
	}
}

// forbiddenChannel matches updateChannel for channel id with the channel as
// channelForbidden.
func forbiddenChannel(id int64) func(*mtproto.Updates) bool {
	return func(us *mtproto.Updates) bool {
		var updated, forbidden bool
		for _, u := range us.GetUpdates() {
			updated = updated || u.GetPredicateName() == mtproto.Predicate_updateChannel && u.GetChannelId() == id
		}
		for _, c := range us.GetChats() {
			forbidden = forbidden || c.GetPredicateName() == mtproto.Predicate_channelForbidden && c.GetId() == id
		}
		return updated && forbidden
	}
}
