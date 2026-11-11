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
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// futureCreator asks who owns the group after u, its owner, leaves it
// (messages.getFutureChatCreatorAfterLeave, as tdesktop and iOS ask it).
func futureCreator(ctx context.Context, u *user, peer *mtproto.InputPeer) (*mtproto.User, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetFutureChatCreatorAfterLeave{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getFutureChatCreatorAfterLeave),
		Peer:        peer,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_User() *mtproto.User }).To_User(), nil
}

// TestCreatorLeavesBasicGroup (task 078): the owner of a basic group asks
// who takes it, an admin before the members; a member may not ask; the
// owner leaves and that admin is the creator at once.
func TestCreatorLeavesBasicGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	chatId := createChat(ctx, t, alice, fmt.Sprintf("leave %d", randomID()), bob, carol)
	editChatAdmin(ctx, t, alice, chatId, inputUserOf(ctx, t, alice, carol), true)

	next, err := futureCreator(ctx, alice, chatPeer(alice, chatId))
	if err != nil {
		t.Fatalf("messages.getFutureChatCreatorAfterLeave: %v", err)
	}
	if next.GetId() != carol.self.GetId() {
		t.Errorf("the next owner %d, want the admin %d", next.GetId(), carol.self.GetId())
	}
	if _, err = futureCreator(ctx, bob, chatPeer(bob, chatId)); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member asks: %v, want CHAT_ADMIN_REQUIRED", err)
	}

	getState(ctx, t, carol)
	deleteChatUser(ctx, t, alice, chatId, &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)})
	waitUpdate(ctx, t, carol, mtproto.Predicate_updateChatParticipants, func(u *mtproto.Update) bool {
		return u.GetParticipants_CHATPARTICIPANTS().GetChatId() == chatId
	})
	if c := chatOf(ctx, t, carol, chatId); !c.GetCreator() {
		t.Errorf("Carol is not the creator after Alice left: %v", c)
	}
}

// TestCreatorLeavesSupergroup (task 078): the owner of a private supergroup
// asks who takes it, by both methods; it leaves, the member is told by
// messageActionNewCreatorPending, and the owner comes back to its group by
// itself, still its owner.
func TestCreatorLeavesSupergroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	// channels.getFutureCreatorAfterLeave is a method of layer 228, which
	// Android 12.9.1 sends (TLRPC.java:70127 at aac1efcb); layer 233 has
	// only messages.getFutureChatCreatorAfterLeave.
	alice := loginAtLayer(ctx, t, phones[0], "Alice", 228)
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("leave %d", randomID()), true)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     input(alice),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetFutureCreatorAfterLeave{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_getFutureCreatorAfterLeave),
		Channel:     input(alice),
	})
	if err != nil {
		t.Fatalf("channels.getFutureCreatorAfterLeave: %v", err)
	}
	if id := o.(interface{ To_User() *mtproto.User }).To_User().GetId(); id != bob.self.GetId() {
		t.Errorf("the next owner %d, want Bob %d", id, bob.self.GetId())
	}
	if next, err := futureCreator(ctx, alice, peer(alice)); err != nil || next.GetId() != bob.self.GetId() {
		t.Errorf("messages.getFutureChatCreatorAfterLeave of the supergroup: %v, %v", next, err)
	}
	if _, err = futureCreator(ctx, bob, peer(bob)); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member asks: %v, want CHAT_ADMIN_REQUIRED", err)
	}

	getState(ctx, t, bob)
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsLeaveChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_leaveChannel),
		Channel:     input(alice),
	}); err != nil {
		t.Fatalf("the owner leaves: %v", err)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionNewCreatorPending && a.GetNewCreatorId() == bob.self.GetId()
	})

	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel24B524C5{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_joinChannel24B524C5),
		Channel:     input(alice),
	})
	if err != nil {
		t.Fatalf("the owner comes back: %v", err)
	}
	if chats := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats(); len(chats) != 1 || !chats[0].GetCreator() || chats[0].GetLeft() {
		t.Errorf("the group to its owner back: %v", chats)
	}
	if next, err := futureCreator(ctx, alice, peer(alice)); err != nil || next.GetId() != bob.self.GetId() {
		t.Errorf("the owner back asks again: %v, %v", next, err)
	}
}

// TestChannelMainTab (task 078): the owner of a channel sets the tab its
// profile opens on, which the full channel then has; a subscriber may not.
// channels.getInactiveChannels lists the owner's channels with a date each.
func TestChannelMainTab(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("tab %d", randomID()), false)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	setTab := func(u *user, tab string) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLChannelsSetMainProfileTab{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_setMainProfileTab),
			Channel:     input(u),
			Tab:         &mtproto.ProfileTab{Constructor: u.c.Constructor(tab)},
		})
		return err
	}
	if err := setTab(bob, mtproto.Predicate_profileTabMedia); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber sets the tab: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if err := setTab(alice, mtproto.Predicate_profileTabFiles); err != nil {
		t.Fatalf("channels.setMainProfileTab: %v", err)
	}
	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input(bob),
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	full := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat()
	if got := full.GetMainTab().GetPredicateName(); got != mtproto.Predicate_profileTabFiles {
		t.Errorf("main_tab %q, want profileTabFiles", got)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetInactiveChannels{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_getInactiveChannels),
	})
	if err != nil {
		t.Fatalf("channels.getInactiveChannels: %v", err)
	}
	inactive := o.(interface {
		To_Messages_InactiveChats() *mtproto.Messages_InactiveChats
	}).To_Messages_InactiveChats()
	if len(inactive.GetChats()) != len(inactive.GetDates()) || !hasChat(inactive.GetChats(), ch.GetId()) {
		t.Errorf("the inactive channels: %d chats, %d dates, without %d", len(inactive.GetChats()), len(inactive.GetDates()), ch.GetId())
	}
}
