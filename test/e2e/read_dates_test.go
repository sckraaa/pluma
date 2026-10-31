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

// Who read a message and when (task 020): in a basic group the members who
// read the sender's message, each with the date they read it, the latest
// first, where a member's date was lost before when another had read the
// message already; in a private chat the date the peer read it. Neither is
// given for a message the asker received.
func TestReadDates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	group := createChat(ctx, t, alice, "Read dates", bob, carol)
	getState(ctx, t, bob)
	getState(ctx, t, carol)

	before := int32(time.Now().Unix())
	text := fmt.Sprintf("who read this %d", randomID())
	sent := sendMessageID(ctx, t, alice, chatPeer(alice, group), text, randomID())
	var bobsCopy int32
	for i, u := range []*user{bob, carol} {
		id := waitMessageId(ctx, t, u, text)
		readHistory(ctx, t, u, chatPeer(u, group))
		if i == 0 {
			bobsCopy = id
			// A second between the two reads orders them.
			time.Sleep(1100 * time.Millisecond)
		}
	}

	// The senders learn of the reads with inbox events after the answer.
	var readers []*mtproto.ReadParticipantDate
	for deadline := time.Now().Add(30 * time.Second); len(readers) < 2 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		raw, err := alice.c.InvokeRaw(ctx, &mtproto.TLMessagesGetMessageReadParticipants31C1C44F{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessageReadParticipants31C1C44F),
			Peer:        chatPeer(alice, group),
			MsgId:       sent,
		})
		if err != nil {
			t.Fatalf("messages.getMessageReadParticipants: %v", err)
		}
		list, err := mtclient.DecodeVector(raw)
		if err != nil {
			t.Fatal(err)
		}
		readers = readers[:0]
		for _, o := range list {
			r, ok := o.(interface {
				To_ReadParticipantDate() *mtproto.ReadParticipantDate
			})
			if !ok {
				t.Fatalf("messages.getMessageReadParticipants holds %T", o)
			}
			readers = append(readers, r.To_ReadParticipantDate())
		}
	}
	now := int32(time.Now().Unix())
	if len(readers) != 2 || readers[0].GetUserId() != carol.self.GetId() || readers[1].GetUserId() != bob.self.GetId() {
		t.Fatalf("readers %v: want Carol, then Bob", readers)
	}
	if b, c := readers[1].GetDate(), readers[0].GetDate(); b < before || c > now || c <= b {
		t.Errorf("Bob read at %d, Carol at %d: want %d <= Bob < Carol <= %d", b, c, before, now)
	}
	if _, err := bob.c.InvokeRaw(ctx, &mtproto.TLMessagesGetMessageReadParticipants31C1C44F{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getMessageReadParticipants31C1C44F),
		Peer:        chatPeer(bob, group),
		MsgId:       bobsCopy,
	}); !rpcErrorIs(err, "MSG_ID_INVALID") {
		t.Errorf("readers of a message Bob received: %v, want MSG_ID_INVALID", err)
	}

	toBob := peerOf(ctx, t, alice, bob)
	text = fmt.Sprintf("read this %d", randomID())
	sent = sendMessageID(ctx, t, alice, toBob, text, randomID())
	received := waitMessageId(ctx, t, bob, text)
	readHistory(ctx, t, bob, peerOf(ctx, t, bob, alice))
	var date int32
	for deadline := time.Now().Add(30 * time.Second); date == 0 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetOutboxReadDate{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getOutboxReadDate),
			Peer:        toBob,
			MsgId:       sent,
		})
		if rpcErrorIs(err, "MESSAGE_NOT_READ_YET") {
			continue
		} else if err != nil {
			t.Fatalf("messages.getOutboxReadDate: %v", err)
		}
		r, ok := o.(interface {
			To_OutboxReadDate() *mtproto.OutboxReadDate
		})
		if !ok {
			t.Fatalf("messages.getOutboxReadDate returned %T", o)
		}
		date = r.To_OutboxReadDate().GetDate()
	}
	if date < before || date > int32(time.Now().Unix()) {
		t.Errorf("Bob read the private message at %d, want within [%d, now]", date, before)
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetOutboxReadDate{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getOutboxReadDate),
		Peer:        peerOf(ctx, t, bob, alice),
		MsgId:       received,
	}); !rpcErrorIs(err, "MESSAGE_ID_INVALID") {
		t.Errorf("read date of a message Bob received: %v, want MESSAGE_ID_INVALID", err)
	}
}

// waitMessageId waits for the push of a new message with text on u and
// returns its id in u's box.
func waitMessageId(ctx context.Context, t *testing.T, u *user, text string) int32 {
	t.Helper()
	var id int32
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, m := range newMessages(us) {
			if m.GetMessage() == text {
				id = m.GetId()
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("no message %q: %v", text, err)
	}
	return id
}

// readHistory reads u's dialog with peer to its end.
func readHistory(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesReadHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_readHistory),
		Peer:        peer,
		MaxId:       1 << 30,
	}); err != nil {
		t.Fatalf("messages.readHistory: %v", err)
	}
}
