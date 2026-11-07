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

// TestChannelPin (task 043) end to end: in a supergroup whose members may
// not pin, Bob may not; Alice, its creator, pins her message: the answer
// and Bob's devices get updatePinnedChannelMessages and the service
// message, Bob finds it with the pinned filter of messages.search and as
// pinned_msg_id of the full channel; Alice unpins everything and the
// filter finds nothing.
func TestChannelPin(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "pins", true)
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
	getState(ctx, t, bob)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesEditChatDefaultBannedRights{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_editChatDefaultBannedRights),
		Peer:         peer(alice),
		BannedRights: &mtproto.ChatBannedRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatBannedRights), PinMessages: true},
	}); err != nil {
		t.Fatalf("messages.editChatDefaultBannedRights: %v", err)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer(alice), Message: "read the rules", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	var id int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
			id = u.GetId_INT32()
		}
	}
	if id == 0 {
		t.Fatal("no id of the sent message")
	}
	pin := func(u *user, unpin bool) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesUpdatePinnedMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_updatePinnedMessage),
			Peer:        peer(u), Id: id, Unpin: unpin,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	if _, err = pin(bob, false); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member pins where members may not: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	r, err := pin(alice, false)
	if err != nil {
		t.Fatalf("messages.updatePinnedMessage: %v", err)
	}
	var pinned, service bool
	for _, u := range r.GetUpdates() {
		pinned = pinned || u.GetPredicateName() == mtproto.Predicate_updatePinnedChannelMessages && u.GetPinned() && len(u.GetMessages()) == 1 && u.GetMessages()[0] == id
		service = service || u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage &&
			u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionPinMessage
	}
	if !pinned || !service {
		t.Errorf("the answer of the pin: %v", r.GetUpdates())
	}
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updatePinnedChannelMessages && u.GetChannelId() == ch.GetId() && u.GetPinned() {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Bob's updatePinnedChannelMessages: %v", err)
	}

	searchPinned := func() []*mtproto.Message {
		t.Helper()
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSearch{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_search),
			Peer:        peer(bob),
			Filter:      &mtproto.MessagesFilter{Constructor: bob.c.Constructor(mtproto.Predicate_inputMessagesFilterPinned)},
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("messages.search (pinned): %v", err)
		}
		return o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages()
	}
	if m := searchPinned(); len(m) != 1 || m[0].GetId() != id || !m[0].GetPinned() {
		t.Errorf("the pinned filter: %v", m)
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input(bob),
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	if got := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetPinnedMsgId().GetValue(); got != id {
		t.Errorf("pinned_msg_id %d, want %d", got, id)
	}

	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesUnpinAllMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_unpinAllMessages),
		Peer:        peer(alice),
	}); err != nil {
		t.Fatalf("messages.unpinAllMessages: %v", err)
	}
	if m := searchPinned(); len(m) != 0 {
		t.Errorf("pinned after unpinning all: %v", m)
	}
}
