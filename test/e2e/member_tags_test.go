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
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

func editRank(ctx context.Context, u *user, peer, participant *mtproto.InputPeer, rank string) error {
	_, err := u.c.Invoke(ctx, &mtproto.TLMessagesEditChatParticipantRank{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_editChatParticipantRank),
		Peer:        peer,
		Participant: participant,
		Rank:        rank,
	})
	return err
}

// The creator sets a tag in a basic group and the next message carries it;
// a member may not set one while the group forbids it.
func TestMemberTagBasicGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	chatId := createChat(ctx, t, alice, fmt.Sprintf("tags %d", randomID()), bob, carol)
	getState(ctx, t, alice)

	// A new group forbids a member its own tag (edit_rank of its default
	// rights); the creator may set anyone's.
	if err := editRank(ctx, bob, chatPeer(bob, chatId), userPeer(bob.c, bob), "Mine"); err == nil {
		t.Error("a member set its own tag though the group forbids it")
	}
	if err := editRank(ctx, bob, chatPeer(bob, chatId), userPeer(bob.c, carol), "No"); err == nil {
		t.Error("a member set the tag of another member")
	}
	if err := editRank(ctx, alice, chatPeer(alice, chatId), userPeer(alice.c, bob), "Tester"); err != nil {
		t.Fatalf("creator's tag on a member: %v", err)
	}
	if err := editRank(ctx, alice, chatPeer(alice, chatId), userPeer(alice.c, carol), "seventeen letters"); err == nil {
		t.Error("a tag over 16 characters passed")
	}

	sendMessageID(ctx, t, bob, chatPeer(bob, chatId), "with a tag", randomID())
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "with a tag" {
			return false
		}
		if got := m.GetFromRank().GetValue(); got != "Tester" {
			t.Errorf("from_rank %q, want Tester", got)
		}
		return true
	})
}

// A member of a supergroup sets its own tag, the next message carries it,
// and the group can forbid it.
func TestMemberTagSupergroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("tags %d", randomID()), true)
	input := &mtproto.InputChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     input,
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{
			Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		}
	}
	getState(ctx, t, alice)

	if err := editRank(ctx, bob, peer(bob), userPeer(bob.c, bob), "Tester"); err != nil {
		t.Fatalf("own tag: %v", err)
	}
	if err := editRank(ctx, bob, peer(bob), userPeer(bob.c, alice), "No"); err == nil {
		t.Error("a member set the tag of the creator")
	}
	sendMessageID(ctx, t, bob, peer(bob), "with a tag", randomID())
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "with a tag" {
			return false
		}
		if got := m.GetFromRank().GetValue(); got != "Tester" {
			t.Errorf("from_rank %q, want Tester", got)
		}
		return true
	})
}

// In a supergroup the sender sees who read its message: Bob reads, Alice
// gets him with the date of his read.
func TestSupergroupReadParticipants(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("reads %d", randomID()), true)
	getState(ctx, t, bob)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{
			Constructor: u.c.Constructor(mtproto.Predicate_inputChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     input(alice),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	peer := &mtproto.InputPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
	}
	// A member added reads from the second of its addition on: the message
	// comes a second later.
	time.Sleep(1100 * time.Millisecond)
	before := int32(time.Now().Unix())
	sent := sendMessageID(ctx, t, alice, peer, "read me", randomID())
	// Bob's box has the dialog once the message reached it: its join came
	// first.
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "read me"
	})
	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsReadHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_readHistory),
		Channel:     input(bob),
		MaxId:       sent,
	}); err != nil {
		t.Fatalf("channels.readHistory: %v", err)
	}
	raw, err := alice.c.InvokeRaw(ctx, &mtproto.TLMessagesGetMessageReadParticipants31C1C44F{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessageReadParticipants31C1C44F),
		Peer:        peer,
		MsgId:       sent,
	})
	if err != nil {
		t.Fatalf("messages.getMessageReadParticipants: %v", err)
	}
	list, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("readers %v: want Bob", list)
	}
	r := list[0].(interface {
		To_ReadParticipantDate() *mtproto.ReadParticipantDate
	}).To_ReadParticipantDate()
	if r.GetUserId() != bob.self.GetId() || r.GetDate() < before {
		t.Errorf("reader %d at %d: want Bob at or after %d", r.GetUserId(), r.GetDate(), before)
	}
}

// An admin takes away a member's reaction in a supergroup; a member may not.
func TestDeleteParticipantReaction(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("reactions %d", randomID()), true)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel: &mtproto.InputChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		},
		Users: []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{
			Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		}
	}
	msgId := sendMessageID(ctx, t, alice, peer(alice), "react to me", randomID())
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
		Constructor:                 bob.c.Constructor(mtproto.Predicate_messages_sendReaction),
		Peer:                        peer(bob),
		MsgId:                       msgId,
		Reaction_FLAGVECTORREACTION: []*mtproto.Reaction{{Constructor: bob.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: "👍"}},
	}); err != nil {
		t.Fatalf("messages.sendReaction: %v", err)
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesDeleteParticipantReaction{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_deleteParticipantReaction),
		Peer:        peer(bob),
		MsgId:       msgId,
		Participant: userPeer(bob.c, alice),
	}); err == nil {
		t.Error("a member took away a reaction")
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteParticipantReaction{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteParticipantReaction),
		Peer:        peer(alice),
		MsgId:       msgId,
		Participant: userPeer(alice.c, bob),
	})
	if err != nil {
		t.Fatalf("messages.deleteParticipantReaction: %v", err)
	}
	r := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageReactions && len(u.GetReactions_MESSAGEREACTIONS().GetResults()) != 0 {
			t.Errorf("reactions left: %v", u.GetReactions_MESSAGEREACTIONS().GetResults())
		}
	}
}

// The recent actions of a supergroup: a new title is there for its admins,
// and a member may not read them.
func TestAdminLog(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("log %d", randomID()), true)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{
			Constructor: u.c.Constructor(mtproto.Predicate_inputChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     input(alice),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsEditTitle{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_editTitle),
		Channel:     input(alice),
		Title:       "renamed",
	}); err != nil {
		t.Fatalf("channels.editTitle: %v", err)
	}
	getLog := func(u *user) (*mtproto.Channels_AdminLogResults, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetAdminLog{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_getAdminLog),
			Channel:     input(u),
			Limit:       20,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Channels_AdminLogResults() *mtproto.Channels_AdminLogResults
		}).To_Channels_AdminLogResults(), nil
	}
	r, err := getLog(alice)
	if err != nil {
		t.Fatalf("channels.getAdminLog: %v", err)
	}
	found := false
	for _, e := range r.GetEvents() {
		a := e.GetAction()
		if a.GetPredicateName() == mtproto.Predicate_channelAdminLogEventActionChangeTitle && a.GetNewValue_STRING() == "renamed" && e.GetUserId() == alice.self.GetId() {
			found = true
		}
	}
	if !found {
		t.Errorf("no new title in the log: %v", r.GetEvents())
	}
	if _, err := getLog(bob); err == nil {
		t.Error("a member read the recent actions")
	}
}
