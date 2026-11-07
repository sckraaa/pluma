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
)

// TestChannelReactions (task 044, the owner's report of 07.11) end to end:
// in a supergroup Bob reacts to Alice's message; his answer shows his
// choice, Alice's devices get updateMessageReactions by push, her
// messages.getMessagesReactions and the history show the count and Bob
// among who reacted, and the list of who reacted names him; Bob takes the
// reaction back and the count is gone.
func TestChannelReactions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "reactions", true)
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
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer(alice), Message: "react to me", RandomId: randomID(),
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
	getState(ctx, t, alice)

	react := func(emoji ...string) *mtproto.Updates {
		t.Helper()
		var list []*mtproto.Reaction
		for _, e := range emoji {
			list = append(list, &mtproto.Reaction{Constructor: bob.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: e})
		}
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
			Constructor:                 bob.c.Constructor(mtproto.Predicate_messages_sendReaction),
			Peer:                        peer(bob),
			MsgId:                       id,
			Reaction_FLAGVECTORREACTION: list,
		})
		if err != nil {
			t.Fatalf("messages.sendReaction: %v", err)
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	}
	count := func(rs *mtproto.MessageReactions) int32 {
		var n int32
		for _, r := range rs.GetResults() {
			n += r.GetCount()
		}
		return n
	}

	r := react("👍")
	if u := r.GetUpdates(); len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateMessageReactions ||
		count(u[0].GetReactions_MESSAGEREACTIONS()) != 1 || u[0].GetReactions_MESSAGEREACTIONS().GetResults()[0].GetChosenOrder() == nil {
		t.Errorf("Bob's answer: %v", u)
	}
	if _, err = alice.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateMessageReactions && u.GetMsgId_INT32() == id && count(u.GetReactions_MESSAGEREACTIONS()) == 1 {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Alice's updateMessageReactions: %v", err)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesReactions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessagesReactions),
		Peer:        peer(alice), Id: []int32{id},
	})
	if err != nil {
		t.Fatalf("messages.getMessagesReactions: %v", err)
	}
	u := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates()
	if len(u) != 1 || count(u[0].GetReactions_MESSAGEREACTIONS()) != 1 ||
		len(u[0].GetReactions_MESSAGEREACTIONS().GetRecentReactions()) != 1 ||
		u[0].GetReactions_MESSAGEREACTIONS().GetRecentReactions()[0].GetPeerId().GetUserId() != bob.self.GetId() {
		t.Errorf("Alice's getMessagesReactions: %v", u)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer(alice), Limit: 5,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	var inHistory bool
	for _, m := range o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages() {
		inHistory = inHistory || m.GetId() == id && count(m.GetReactions()) == 1
	}
	if !inHistory {
		t.Error("the history does not show the reaction")
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessageReactionsList{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessageReactionsList),
		Peer:        peer(alice), Id: id, Limit: 10,
	})
	if err != nil {
		t.Fatalf("messages.getMessageReactionsList: %v", err)
	}
	if l := o.(interface {
		To_Messages_MessageReactionsList() *mtproto.Messages_MessageReactionsList
	}).To_Messages_MessageReactionsList(); l.GetCount() != 1 || l.GetReactions()[0].GetPeerId().GetUserId() != bob.self.GetId() {
		t.Errorf("who reacted: %v", l)
	}

	if r = react(); count(r.GetUpdates()[0].GetReactions_MESSAGEREACTIONS()) != 0 {
		t.Errorf("after taking it back: %v", r.GetUpdates())
	}
}
