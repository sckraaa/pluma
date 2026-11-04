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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// channelInviteHash is the hash of the channel's permanent link, from the full
// channel of its creator.
func channelInviteHash(ctx context.Context, t *testing.T, u *user, input *mtproto.InputChannel) string {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input,
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	link := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetExportedInvite().GetLink()
	i := strings.LastIndex(link, "/+")
	if i < 0 {
		t.Fatalf("invite link %q", link)
	}
	return link[i+2:]
}

func checkInvite(ctx context.Context, t *testing.T, u *user, hash string) *mtproto.ChatInvite {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesCheckChatInvite{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_checkChatInvite),
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.checkChatInvite: %v", err)
	}
	return o.(interface{ To_ChatInvite() *mtproto.ChatInvite }).To_ChatInvite()
}

// One joins a supergroup and a channel by their links (task 041): the link
// shows the chat, the join answers with the channel and, in a supergroup,
// the service message; the member reads its own participation (iOS after
// joining), gets the posts by push, is in the list of members, may not
// post in a channel; leaving makes the private channel forbidden to it.
func TestChannelJoin(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	for _, megagroup := range []bool{true, false} {
		_, ch := createChannel(ctx, t, alice, "join", megagroup)
		hash := ch.GetAccessHash_FLAGINT64().GetValue()
		input := func(u *user) *mtproto.InputChannel {
			return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
		}
		peer := func(u *user) *mtproto.InputPeer {
			return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
		}
		link := channelInviteHash(ctx, t, alice, input(alice))

		inv := checkInvite(ctx, t, bob, link)
		if inv.GetPredicateName() != mtproto.Predicate_chatInvite || !inv.GetChannel() || inv.GetMegagroup() != megagroup || inv.GetParticipantsCount() != 1 {
			t.Fatalf("chatInvite: %s channel %v megagroup %v members %d", inv.GetPredicateName(), inv.GetChannel(), inv.GetMegagroup(), inv.GetParticipantsCount())
		}
		if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel7F6A1E22{
			Constructor: bob.c.Constructor(mtproto.Predicate_channels_joinChannel7F6A1E22),
			Channel:     input(bob),
		}); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
			t.Errorf("joining a private channel without its link: %v, want CHANNEL_PRIVATE", err)
		}

		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        link,
		})
		if err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
		r := o.(interface {
			To_Messages_ChatInviteJoinResult() *mtproto.Messages_ChatInviteJoinResult
		}).To_Messages_ChatInviteJoinResult().GetUpdates()
		if len(r.GetChats()) != 1 || r.GetChats()[0].GetId() != ch.GetId() || r.GetChats()[0].GetLeft() {
			t.Fatalf("the join's chats: %v", r.GetChats())
		}
		var service bool
		for _, u := range r.GetUpdates() {
			service = service || u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionChatJoinedByLink
		}
		if service != megagroup {
			t.Errorf("service message of the join %v in a megagroup %v", service, megagroup)
		}
		if inv = checkInvite(ctx, t, bob, link); inv.GetPredicateName() != mtproto.Predicate_chatInviteAlready {
			t.Errorf("the link to a member: %s", inv.GetPredicateName())
		}
		if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetParticipant{
			Constructor: bob.c.Constructor(mtproto.Predicate_channels_getParticipant),
			Channel:     input(bob),
			Participant: &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		}); err != nil {
			t.Errorf("channels.getParticipant(self) after joining: %v", err)
		}

		o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetParticipants{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getParticipants),
			Channel:     input(alice),
			Filter:      &mtproto.ChannelParticipantsFilter{Constructor: alice.c.Constructor(mtproto.Predicate_channelParticipantsRecent)},
			Limit:       50,
		})
		if err != nil {
			t.Fatalf("channels.getParticipants: %v", err)
		}
		if ps := o.(interface {
			To_Channels_ChannelParticipants() *mtproto.Channels_ChannelParticipants
		}).To_Channels_ChannelParticipants(); ps.GetCount() != 2 || len(ps.GetUsers()) != 2 {
			t.Errorf("members: count %d, %d users", ps.GetCount(), len(ps.GetUsers()))
		}

		sendMessageID(ctx, t, alice, peer(alice), "after the join", randomID())
		waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
			m := u.GetMessage_MESSAGE()
			return m.GetPeerId().GetChannelId() == ch.GetId() && m.GetMessage() == "after the join"
		})
		_, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer(bob), Message: "from a member", RandomId: randomID(),
		})
		switch {
		case megagroup && err != nil:
			t.Errorf("a member writes to a supergroup: %v", err)
		case !megagroup && !mtclient.IsRPCError(err, "CHAT_WRITE_FORBIDDEN"):
			t.Errorf("a subscriber posts to a channel: %v, want CHAT_WRITE_FORBIDDEN", err)
		}

		o, err = bob.c.Invoke(ctx, &mtproto.TLChannelsLeaveChannel{
			Constructor: bob.c.Constructor(mtproto.Predicate_channels_leaveChannel),
			Channel:     input(bob),
		})
		if err != nil {
			t.Fatalf("channels.leaveChannel: %v", err)
		}
		if chats := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats(); len(chats) != 1 || chats[0].GetPredicateName() != mtproto.Predicate_channelForbidden {
			t.Errorf("the private channel after leaving: %v", chats)
		}
		if _, err = channelDifference(ctx, bob, ch, hash, 1, 10); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
			t.Errorf("the difference after leaving: %v, want CHANNEL_PRIVATE", err)
		}
		if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsLeaveChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_leaveChannel),
			Channel:     input(alice),
		}); !mtclient.IsRPCError(err, "USER_CREATOR") {
			t.Errorf("the creator leaves: %v, want USER_CREATOR", err)
		}
	}
}
