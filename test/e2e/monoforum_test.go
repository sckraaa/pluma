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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The direct messages of a channel (task 084, ADR 0055;
// core.telegram.org/api/monoforum): its owner switches them on, which makes
// the monoforum; two subscribers write to the channel through it and see
// their own chats alone; the owner lists the chats, answers one as the
// channel, and the read marks go both ways.
func TestMonoforum(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	getState(ctx, t, carol)

	_, ch := createChannel(ctx, t, alice, "direct", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	invite := channelInviteHash(ctx, t, alice, input(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        invite,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}

	price := func(u *user, on bool, stars int64) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsUpdatePaidMessagesPrice{
			Constructor:              u.c.Constructor(mtproto.Predicate_channels_updatePaidMessagesPrice),
			BroadcastMessagesAllowed: on,
			Channel:                  input(u),
			SendPaidMessagesStars:    stars,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	if _, err := price(alice, true, 10); !mtclient.IsRPCError(err, "STARS_AMOUNT_INVALID") {
		t.Errorf("a price in stars: %v, want STARS_AMOUNT_INVALID", err)
	}
	if _, err := price(bob, true, 0); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber switches the direct messages on: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	r, err := price(alice, true, 0)
	if err != nil {
		t.Fatalf("channels.updatePaidMessagesPrice: %v", err)
	}
	var mono *mtproto.Chat
	for _, c := range r.GetChats() {
		switch {
		case c.GetId() == ch.GetId():
			if !c.GetBroadcastMessagesAllowed() || c.GetLinkedMonoforumId().GetValue() == 0 {
				t.Errorf("the channel after switching on: %v", c)
			}
		case c.GetMonoforum():
			mono = c
		}
	}
	if mono == nil || mono.GetLinkedMonoforumId().GetValue() != ch.GetId() || !mono.GetMegagroup() {
		t.Fatalf("the monoforum in the answer: %v", r.GetChats())
	}
	if _, err = price(alice, true, 0); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("switching on what is on: %v, want CHAT_NOT_MODIFIED", err)
	}

	// A subscriber finds the monoforum in the full channel.
	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{Constructor: bob.c.Constructor(mtproto.Predicate_channels_getFullChannel), Channel: input(bob)})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	var seen *mtproto.Chat
	for _, c := range o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetChats() {
		if c.GetId() == mono.GetId() {
			seen = c
		}
	}
	if seen == nil || seen.GetPredicateName() != mtproto.Predicate_channel || !seen.GetMonoforum() || seen.GetLeft() || seen.GetAdminRights() != nil {
		t.Fatalf("the monoforum as a subscriber sees it: %v", seen)
	}
	monoHash := mono.GetAccessHash_FLAGINT64().GetValue()
	monoPeer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: mono.GetId(), AccessHash: monoHash}
	}
	monoInput := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: mono.GetId(), AccessHash: monoHash}
	}
	userPeer := func(u, of *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: of.self.GetId(), AccessHash: of.self.GetAccessHash().GetValue()}
	}

	// Bob and Carol write to the channel; Alice gets both, each in its
	// sublist.
	b1 := sendMessageID(ctx, t, bob, monoPeer(bob), "from bob", randomID())
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetPeerId().GetChannelId() == mono.GetId() && m.GetId() == b1 && m.GetSavedPeerId().GetUserId() == bob.self.GetId() && m.GetFromId().GetUserId() == bob.self.GetId()
	})
	c1 := sendMessageID(ctx, t, carol, monoPeer(carol), "from carol", randomID())

	history := func(u *user) []int32 {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory), Peer: monoPeer(u), Limit: 50,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		var ids []int32
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			ids = append(ids, m.GetId())
		}
		slices.Sort(ids)
		return ids
	}
	if got := history(bob); !slices.Equal(got, []int32{b1}) {
		t.Errorf("Bob's history of the monoforum: %v, want his message %d alone", got, b1)
	}
	if got := history(carol); !slices.Equal(got, []int32{c1}) {
		t.Errorf("Carol's history of the monoforum: %v, want her message %d alone", got, c1)
	}
	if got := history(alice); !slices.Equal(got, []int32{b1, c1}) {
		t.Errorf("Alice's history of the monoforum: %v, want both", got)
	}

	// Alice lists the sublists and reads one.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetSavedDialogs{
		Constructor:   alice.c.Constructor(mtproto.Predicate_messages_getSavedDialogs),
		ExcludePinned: true,
		ParentPeer:    monoPeer(alice),
		OffsetPeer:    &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:         20,
	})
	if err != nil {
		t.Fatalf("messages.getSavedDialogs: %v", err)
	}
	list := o.(interface {
		To_Messages_SavedDialogs() *mtproto.Messages_SavedDialogs
	}).To_Messages_SavedDialogs()
	if len(list.GetDialogs()) != 2 || list.GetDialogs()[0].GetPeer().GetUserId() != carol.self.GetId() || list.GetDialogs()[1].GetPeer().GetUserId() != bob.self.GetId() {
		t.Fatalf("the sublists: %v", list.GetDialogs())
	}
	if d := list.GetDialogs()[1]; d.GetPredicateName() != mtproto.Predicate_monoForumDialog || d.GetTopMessage() != b1 || d.GetUnreadCount() != 1 {
		t.Errorf("Bob's sublist: %v", d)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetSavedDialogs{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getSavedDialogs),
		ParentPeer:  monoPeer(bob),
		OffsetPeer:  &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       20,
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber lists the sublists: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetSavedHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getSavedHistory),
		ParentPeer:  monoPeer(alice), Peer: userPeer(alice, bob), Limit: 20,
	})
	if err != nil {
		t.Fatalf("messages.getSavedHistory: %v", err)
	}
	if ms := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(ms) != 1 || ms[0].GetId() != b1 {
		t.Errorf("the history of Bob's sublist: %v", ms)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesReadSavedHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_readSavedHistory),
		ParentPeer:  monoPeer(alice), Peer: userPeer(alice, bob), MaxId: b1,
	}); err != nil {
		t.Fatalf("messages.readSavedHistory: %v", err)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateReadChannelOutbox, func(u *mtproto.Update) bool {
		return u.GetChannelId() == mono.GetId() && u.GetMaxId() == b1
	})

	// Alice answers Bob as the channel; Bob gets it, Carol does not see it.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        monoPeer(alice),
		ReplyTo: &mtproto.InputReplyTo{
			Constructor:     alice.c.Constructor(mtproto.Predicate_inputReplyToMonoForum),
			MonoforumPeerId: userPeer(alice, bob),
		},
		Message:  "from the channel",
		RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage to a sublist: %v", err)
	}
	var answer *mtproto.Message
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			answer = m
		}
	}
	if answer.GetFromId().GetChannelId() != ch.GetId() || answer.GetSavedPeerId().GetUserId() != bob.self.GetId() || !answer.GetOut() {
		t.Fatalf("the channel's answer: %v", answer)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetId() == answer.GetId() && m.GetFromId().GetChannelId() == ch.GetId() && !m.GetOut()
	})
	if got := history(carol); !slices.Equal(got, []int32{c1}) {
		t.Errorf("Carol's history after the answer to Bob: %v", got)
	}
	if got := history(bob); !slices.Equal(got, []int32{b1, answer.GetId()}) {
		t.Errorf("Bob's history after the answer: %v", got)
	}
	if ds := peerDialogs(ctx, t, bob, monoPeer(bob)).GetDialogs(); len(ds) != 1 || ds[0].GetUnreadCount() != 1 || ds[0].GetTopMessage() != answer.GetId() {
		t.Errorf("Bob's dialog with the monoforum: %v", ds)
	}

	// Bob reads; Alice learns how far.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsReadHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_readHistory), Channel: monoInput(bob), MaxId: answer.GetId(),
	}); err != nil {
		t.Fatalf("channels.readHistory: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateReadMonoForumOutbox, func(u *mtproto.Update) bool {
		return u.GetChannelId() == mono.GetId() && u.GetSavedPeerId().GetUserId() == bob.self.GetId() && u.GetReadMaxId() == answer.GetId()
	})

	// Who answered as the channel.
	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetMessageAuthor{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_getMessageAuthor), Channel: monoInput(alice), Id: answer.GetId(),
	})
	if err != nil {
		t.Fatalf("channels.getMessageAuthor: %v", err)
	}
	if author := o.(interface{ To_User() *mtproto.User }).To_User(); author.GetId() != alice.self.GetId() {
		t.Errorf("the author of the answer: %v", author)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetMessageAuthor{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getMessageAuthor), Channel: monoInput(bob), Id: answer.GetId(),
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber asks who answered: %v, want CHAT_ADMIN_REQUIRED", err)
	}

	// Carol types to the channel: the admins see it, Bob does not.
	if _, err = carol.c.Invoke(ctx, &mtproto.TLMessagesSetTyping{
		Constructor: carol.c.Constructor(mtproto.Predicate_messages_setTyping),
		Peer:        monoPeer(carol),
		Action:      &mtproto.SendMessageAction{Constructor: carol.c.Constructor(mtproto.Predicate_sendMessageTypingAction)},
	}); err != nil {
		t.Fatalf("messages.setTyping: %v", err)
	}
	typing := func(us *mtproto.Updates) bool {
		u := us.GetUpdate()
		return u.GetPredicateName() == mtproto.Predicate_updateChannelUserTyping && u.GetChannelId() == mono.GetId() && u.GetFromId().GetUserId() == carol.self.GetId()
	}
	if _, err = alice.c.WaitUpdate(ctx, typing); err != nil {
		t.Fatalf("no typing of Carol for Alice: %v", err)
	}
	quiet, stop := context.WithTimeout(ctx, 3*time.Second)
	if _, err = bob.c.WaitUpdate(quiet, typing); err == nil {
		t.Error("Bob saw Carol typing to the channel")
	}
	stop()

	// Who else writes to the channel, and who answers, is for the admins.
	participant := func(u, of *user) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetParticipant{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_getParticipant),
			Channel:     monoInput(u), Participant: userPeer(u, of),
		})
		return err
	}
	if err = participant(bob, alice); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber asks about the owner: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if err = participant(alice, bob); err != nil {
		t.Errorf("the owner asks about a subscriber: %v", err)
	}

	// Alice searches Bob's chat alone (its search and shared media).
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSearch{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_search),
		Peer:        monoPeer(alice), SavedPeerId: userPeer(alice, bob),
		Filter: &mtproto.MessagesFilter{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)},
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("messages.search in a sublist: %v", err)
	}
	var found []int32
	for _, m := range o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages() {
		found = append(found, m.GetId())
	}
	slices.Sort(found)
	if !slices.Equal(found, []int32{b1, answer.GetId()}) {
		t.Errorf("the search in Bob's chat: %v, want %d and %d", found, b1, answer.GetId())
	}

	// Bob removes the chat with the channel ("Leave" in tdesktop); the
	// channel's next answer brings it back.
	inDialogs := func(u *user) bool {
		t.Helper()
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
			if d.GetPeer().GetChannelId() == mono.GetId() {
				return true
			}
		}
		return false
	}
	if !inDialogs(bob) {
		t.Fatal("Bob has no dialog with the monoforum before leaving it")
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsLeaveChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_leaveChannel), Channel: monoInput(bob),
	}); err != nil {
		t.Fatalf("channels.leaveChannel of the monoforum: %v", err)
	}
	if inDialogs(bob) {
		t.Error("the monoforum is in Bob's dialogs after he left it")
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        monoPeer(alice),
		ReplyTo:     &mtproto.InputReplyTo{Constructor: alice.c.Constructor(mtproto.Predicate_inputReplyToMonoForum), MonoforumPeerId: userPeer(alice, bob)},
		Message:     "once more", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage to Bob's sublist again: %v", err)
	}
	var again int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			again = m.GetId()
		}
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetId() == again
	})
	deadline := time.Now().Add(10 * time.Second)
	for !inDialogs(bob) {
		if time.Now().After(deadline) {
			t.Fatal("the channel's answer did not bring the monoforum back to Bob's dialogs")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Switched off, nobody writes.
	if _, err = price(alice, false, 0); err != nil {
		t.Fatalf("switching the direct messages off: %v", err)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        monoPeer(bob), Message: "late", RandomId: randomID(),
	}); !mtclient.IsRPCError(err, "CHAT_WRITE_FORBIDDEN") {
		t.Errorf("a message to direct messages switched off: %v, want CHAT_WRITE_FORBIDDEN", err)
	}
}
