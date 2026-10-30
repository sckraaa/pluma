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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Forwarding (task 020): "Hide sender name" (drop_author) forwards a copy
// without the header of the author; a forward of a user whose forwards
// privacy hides the account names them in from_name.
func TestForwardAuthor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	text := fmt.Sprintf("forward me %d", randomID())
	sendMessageID(ctx, t, alice, peerOf(ctx, t, alice, bob), text, randomID())
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountSetPrivacy{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_setPrivacy),
		Key:         &mtproto.InputPrivacyKey{Constructor: alice.c.Constructor(mtproto.Predicate_inputPrivacyKeyForwards)},
		Rules:       []*mtproto.InputPrivacyRule{{Constructor: alice.c.Constructor(mtproto.Predicate_inputPrivacyValueDisallowAll)}},
	}); err != nil {
		t.Fatalf("account.setPrivacy: %v", err)
	}

	// Bob's copy arrives with an inbox event after Alice's reply.
	toAlice := peerOf(ctx, t, bob, alice)
	var id int32
	for deadline := time.Now().Add(30 * time.Second); id == 0 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        toAlice,
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			if m.GetMessage() == text {
				id = m.GetId()
			}
		}
	}
	if id == 0 {
		t.Fatal("Alice's message did not reach Bob")
	}

	forward := func(dropAuthor bool) *mtproto.Message {
		t.Helper()
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_forwardMessages),
			DropAuthor:  dropAuthor,
			FromPeer:    toAlice,
			Id:          []int32{id},
			RandomId:    []int64{randomID()},
			ToPeer:      &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		})
		if err != nil {
			t.Fatalf("messages.forwardMessages (drop_author %v): %v", dropAuthor, err)
		}
		msgs := newMessages(o.(interface{ To_Updates() *mtproto.Updates }).To_Updates())
		if len(msgs) != 1 || msgs[0].GetMessage() != text {
			t.Fatalf("messages.forwardMessages (drop_author %v): %d messages", dropAuthor, len(msgs))
		}
		return msgs[0]
	}
	name := strings.TrimSpace(alice.self.GetFirstName().GetValue() + " " + alice.self.GetLastName().GetValue())
	if h := forward(false).GetFwdFrom(); h == nil || h.GetFromId() != nil || h.GetFromName().GetValue() != name {
		t.Errorf("the forward of a hidden author: from %v, name %q, want %q", h.GetFromId(), h.GetFromName().GetValue(), name)
	}
	if h := forward(true).GetFwdFrom(); h != nil {
		t.Errorf("a forward without the sender name has the header %v", h)
	}
}
