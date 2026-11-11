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

// A member sets its own tag in a basic group; the next message carries it, a
// tag on another member is refused, and the creator may set one.
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

	if err := editRank(ctx, bob, chatPeer(bob, chatId), userPeer(bob.c, bob), "Tester"); err != nil {
		t.Fatalf("own tag: %v", err)
	}
	if err := editRank(ctx, bob, chatPeer(bob, chatId), userPeer(bob.c, carol), "No"); err == nil {
		t.Error("a member set the tag of another member")
	}
	if err := editRank(ctx, alice, chatPeer(alice, chatId), userPeer(alice.c, carol), "Guest"); err != nil {
		t.Errorf("creator's tag on a member: %v", err)
	}
	if err := editRank(ctx, bob, chatPeer(bob, chatId), userPeer(bob.c, bob), "seventeen letters"); err == nil {
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
		AccessHash:  ch.GetAccessHash().GetValue(),
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
			AccessHash:  ch.GetAccessHash().GetValue(),
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
