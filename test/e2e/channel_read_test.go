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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// A member reads a supergroup (task 043): its dialog counts the unread
// messages until then and none after; its other device gets
// updateReadChannelInbox, the author updateReadChannelOutbox; reading
// again changes nothing.
func TestChannelRead(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bobLaptop := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bobLaptop)

	_, ch := createChannel(ctx, t, alice, "read", true)
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
	var last int32
	for _, text := range []string{"one", "two", "three"} {
		last = sendMessageID(ctx, t, alice, peer(alice), text, randomID())
	}
	dialog := func() *mtproto.Dialog {
		t.Helper()
		ds := peerDialogs(ctx, t, bob, peer(bob)).GetDialogs()
		if len(ds) != 1 {
			t.Fatalf("Bob's dialogs with the channel: %d", len(ds))
		}
		return ds[0]
	}
	if d := dialog(); d.GetUnreadCount() != 3 || d.GetTopMessage() != last {
		t.Fatalf("before reading: unread %d, top %d (want 3, %d)", d.GetUnreadCount(), d.GetTopMessage(), last)
	}

	read := func(maxId int32) {
		t.Helper()
		o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsReadHistory{
			Constructor: bob.c.Constructor(mtproto.Predicate_channels_readHistory),
			Channel:     input(bob),
			MaxId:       maxId,
		})
		if err != nil {
			t.Fatalf("channels.readHistory: %v", err)
		}
		if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
			t.Errorf("channels.readHistory answered %v", o)
		}
	}
	read(last - 1)
	if d := dialog(); d.GetUnreadCount() != 1 || d.GetReadInboxMaxId() != last-1 {
		t.Errorf("after reading to %d: unread %d, read %d", last-1, d.GetUnreadCount(), d.GetReadInboxMaxId())
	}
	waitUpdate(ctx, t, bobLaptop, mtproto.Predicate_updateReadChannelInbox, func(u *mtproto.Update) bool {
		return u.GetChannelId() == ch.GetId() && u.GetMaxId() == last-1 && u.GetStillUnreadCount() == 1
	})
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateReadChannelOutbox, func(u *mtproto.Update) bool {
		return u.GetChannelId() == ch.GetId() && u.GetMaxId() == last-1
	})
	read(0)
	if d := dialog(); d.GetUnreadCount() != 0 || d.GetReadInboxMaxId() != last {
		t.Errorf("after reading all: unread %d, read %d", d.GetUnreadCount(), d.GetReadInboxMaxId())
	}
	read(1) // behind the read: nothing changes
	if d := dialog(); d.GetReadInboxMaxId() != last {
		t.Errorf("a read behind moved the dialog to %d", d.GetReadInboxMaxId())
	}
}
