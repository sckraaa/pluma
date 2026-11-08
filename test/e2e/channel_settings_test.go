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

// TestChannelSettings (task 044) end to end: Alice signs her channel's
// posts, and a post names her; she forbids forwards, and Bob's forward is
// CHAT_FORWARDS_RESTRICTED; she allows one reaction, and Bob's other one is
// REACTION_INVALID; the same setting again is CHAT_NOT_MODIFIED; Bob, a
// subscriber, changes nothing. In her supergroup she hides the history
// from new members; its members are not hidden below 100 members.
func TestChannelSettings(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "settings", false)
	_, gr := createChannel(ctx, t, alice, "settings chat", true)
	inputOf := func(u *user, c *mtproto.Chat) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	peerOf := func(u *user, c *mtproto.Chat) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, inputOf(alice, ch)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	signatures := func(u *user, on bool) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLChannelsToggleSignatures{
			Constructor:       u.c.Constructor(mtproto.Predicate_channels_toggleSignatures),
			Channel:           inputOf(u, ch),
			SignaturesEnabled: on,
		})
		return err
	}
	if err := signatures(bob, true); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber signs the posts: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if err := signatures(alice, true); err != nil {
		t.Fatalf("channels.toggleSignatures: %v", err)
	}
	if err := signatures(alice, true); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("signatures again: %v, want CHAT_NOT_MODIFIED", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOf(alice, ch), Message: "signed", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("the post: %v", err)
	}
	var post int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			post = u.GetMessage_MESSAGE().GetId()
			if a := u.GetMessage_MESSAGE().GetPostAuthor().GetValue(); a != "Alice" {
				t.Errorf("the post's author %q, want Alice", a)
			}
		}
	}

	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesToggleNoForwards{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_toggleNoForwards),
		Peer:        peerOf(alice, ch), Enabled: mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("messages.toggleNoForwards: %v", err)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    peerOf(bob, ch), Id: []int32{post}, RandomId: []int64{randomID()},
		ToPeer: &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)},
	}); !mtclient.IsRPCError(err, "CHAT_FORWARDS_RESTRICTED") {
		t.Errorf("Bob forwards from a channel that forbids it: %v, want CHAT_FORWARDS_RESTRICTED", err)
	}

	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSetChatAvailableReactions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_setChatAvailableReactions),
		Peer:        peerOf(alice, ch),
		AvailableReactions_CHATREACTIONS: mtproto.MakeTLChatReactionsSome(&mtproto.ChatReactions{
			Reactions: []*mtproto.Reaction{{Constructor: alice.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: "👍"}},
		}).To_ChatReactions(),
	}); err != nil {
		t.Fatalf("messages.setChatAvailableReactions: %v", err)
	}
	react := func(emoji string) error {
		_, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendReaction),
			Peer:        peerOf(bob, ch), MsgId: post,
			Reaction_FLAGVECTORREACTION: []*mtproto.Reaction{{Constructor: bob.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: emoji}},
		})
		return err
	}
	if err = react("❤"); !mtclient.IsRPCError(err, "REACTION_INVALID") {
		t.Errorf("a reaction the channel does not allow: %v, want REACTION_INVALID", err)
	}
	if err = react("👍"); err != nil {
		t.Errorf("the reaction it allows: %v", err)
	}

	toggle := func(m mtproto.TLObject) error {
		_, err := alice.c.Invoke(ctx, m)
		return err
	}
	if err = toggle(&mtproto.TLChannelsTogglePreHistoryHidden{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_togglePreHistoryHidden),
		Channel:     inputOf(alice, gr), Enabled: mtproto.BoolTrue,
	}); err != nil {
		t.Errorf("channels.togglePreHistoryHidden: %v", err)
	}
	if err = toggle(&mtproto.TLChannelsToggleParticipantsHidden{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleParticipantsHidden),
		Channel:     inputOf(alice, gr), Enabled: mtproto.BoolTrue,
	}); !mtclient.IsRPCError(err, "PARTICIPANTS_TOO_FEW") {
		t.Errorf("members hidden in a small group: %v, want PARTICIPANTS_TOO_FEW", err)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     inputOf(alice, gr),
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	if !o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetHiddenPrehistory() {
		t.Error("the history is not hidden from new members")
	}
}
