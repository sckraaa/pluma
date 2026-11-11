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

// TestChannelTextEntities: a message to a supergroup keeps the mention, the
// link and the hashtag of its text as entities, as one to a user or a chat
// does (the owner's report of 11.11: "@support" in a comment was a link only
// until the comments were opened again), and the mentioned member has it
// as an unread mention.
func TestChannelTextEntities(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	name := fmt.Sprintf("bob_e2e_%d", bob.self.GetId())
	if _, err := bob.c.Invoke(ctx, &mtproto.TLAccountUpdateUsername{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_updateUsername),
		Username:    name,
	}); err != nil {
		t.Fatalf("account.updateUsername: %v", err)
	}

	_, gr := createChannel(ctx, t, alice, "entities", true)
	hash := gr.GetAccessHash_FLAGINT64().GetValue()
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: hash},
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	peerOf := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}

	text := "@" + name + " see https://pluma.chat #news"
	id := sendMessageID(ctx, t, alice, peerOf(alice), text, randomID())

	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetMessages{
		Constructor:           bob.c.Constructor(mtproto.Predicate_channels_getMessages),
		Channel:               &mtproto.InputChannel{Constructor: bob.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: hash},
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{mtproto.MakeTLInputMessageID(&mtproto.InputMessage{Id: id}).To_InputMessage()},
	})
	if err != nil {
		t.Fatalf("channels.getMessages: %v", err)
	}
	ms := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
	if len(ms) != 1 {
		t.Fatalf("channels.getMessages: %d messages", len(ms))
	}
	kinds := map[string]bool{}
	for _, e := range ms[0].GetEntities() {
		kinds[e.GetPredicateName()] = true
	}
	for _, want := range []string{mtproto.Predicate_messageEntityMention, mtproto.Predicate_messageEntityUrl, mtproto.Predicate_messageEntityHashtag} {
		if !kinds[want] {
			t.Errorf("the message read back has no %s: %v", want, ms[0].GetEntities())
		}
	}
	if !ms[0].GetMentioned() {
		t.Error("the message does not say it mentions Bob")
	}
	if ds := peerDialogs(ctx, t, bob, peerOf(bob)).GetDialogs(); len(ds) != 1 || ds[0].GetUnreadMentionsCount() != 1 {
		t.Errorf("Bob's dialog: %v, want one unread mention", ds)
	}
}
