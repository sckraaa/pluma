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

// TestChannelRights (task 042) end to end: in a supergroup Alice, its
// creator, forbids Bob to send photos and he still writes text; she
// promotes Carol with ban_users, who mutes Bob and then kicks him: his
// devices get updateChannel with the channel as channelForbidden, he
// writes nothing and may not join again by the link. Alice's default
// restrictions hold for Carol's peers, not for Carol.
func TestChannelRights(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "rights", true)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	link := channelInviteHash(ctx, t, alice, input(alice))
	join := func(u *user) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        link,
		})
		return err
	}
	for _, u := range []*user{bob, carol} {
		if err := join(u); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	userPeer := func(from, to *user) *mtproto.InputPeer {
		in := inputUserOf(ctx, t, from, to)
		return &mtproto.InputPeer{Constructor: from.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: in.UserId, AccessHash: in.AccessHash}
	}
	ban := func(by, who *user, rights *mtproto.ChatBannedRights) error {
		rights.Constructor = by.c.Constructor(mtproto.Predicate_chatBannedRights)
		_, err := by.c.Invoke(ctx, &mtproto.TLChannelsEditBanned{
			Constructor:  by.c.Constructor(mtproto.Predicate_channels_editBanned),
			Channel:      input(by),
			Participant:  userPeer(by, who),
			BannedRights: rights,
		})
		return err
	}
	send := func(u *user, text string) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer(u), Message: text, RandomId: randomID(),
		})
		return err
	}

	if err := ban(alice, bob, &mtproto.ChatBannedRights{SendPhotos: true}); err != nil {
		t.Fatalf("channels.editBanned (photos): %v", err)
	}
	if err := send(bob, "text is allowed"); err != nil {
		t.Errorf("Bob without photos writes text: %v", err)
	}
	if err := ban(carol, bob, &mtproto.ChatBannedRights{SendMessages: true}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member restricts: %v, want CHAT_ADMIN_REQUIRED", err)
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsEditAdmin{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_editAdmin),
		Channel:     input(alice),
		UserId:      inputUserOf(ctx, t, alice, carol),
		AdminRights: &mtproto.ChatAdminRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatAdminRights), BanUsers: true},
	}); err != nil {
		t.Fatalf("channels.editAdmin: %v", err)
	}
	if err := ban(carol, bob, &mtproto.ChatBannedRights{SendMessages: true}); err != nil {
		t.Fatalf("Carol mutes Bob: %v", err)
	}
	if err := send(bob, "muted"); !mtclient.IsRPCError(err, "CHAT_WRITE_FORBIDDEN") {
		t.Errorf("a muted member writes: %v, want CHAT_WRITE_FORBIDDEN", err)
	}

	if err := ban(carol, bob, &mtproto.ChatBannedRights{ViewMessages: true, SendMessages: true}); err != nil {
		t.Fatalf("Carol kicks Bob: %v", err)
	}
	if _, err := bob.c.WaitUpdate(ctx, forbiddenChannel(ch.GetId())); err != nil {
		t.Fatalf("Bob's updateChannel with channelForbidden: %v", err)
	}
	if err := send(bob, "kicked"); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
		t.Errorf("a kicked user writes: %v, want CHANNEL_PRIVATE", err)
	}
	if err := join(bob); err == nil {
		t.Error("a kicked user joins again by the link")
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesEditChatDefaultBannedRights{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_editChatDefaultBannedRights),
		Peer:         peer(alice),
		BannedRights: &mtproto.ChatBannedRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatBannedRights), SendMessages: true},
	}); err != nil {
		t.Fatalf("messages.editChatDefaultBannedRights: %v", err)
	}
	if err := send(carol, "an admin writes"); err != nil {
		t.Errorf("the defaults hold an admin back: %v", err)
	}
}
