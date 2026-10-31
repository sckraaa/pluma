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
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The days chosen in the calendar of a private chat go for both users with
// "also delete for" (messages.deleteHistory with min_date, max_date and
// revoke, task 020).
func TestDeleteHistoryOfDays(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob, toAlice := peerOf(ctx, t, alice, bob), peerOf(ctx, t, bob, alice)
	text := fmt.Sprintf("today %d", randomID())
	sendMessageID(ctx, t, alice, toBob, text, randomID())
	// Bob's copy arrives with an inbox event after Alice's reply.
	for deadline := time.Now().Add(30 * time.Second); len(historyTexts(ctx, t, bob, toAlice)) == 0 && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
	}

	now := time.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteHistory),
		Revoke:      true,
		Peer:        toBob,
		MinDate:     wrapperspb.Int32(int32(day.Unix())),
		MaxDate:     wrapperspb.Int32(int32(day.Add(24*time.Hour).Unix() - 1)),
	})
	if err != nil {
		t.Fatalf("messages.deleteHistory: %v", err)
	}
	if a := o.(interface {
		To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
	}).To_Messages_AffectedHistory(); a.GetPtsCount() != 1 || a.GetOffset() != 0 {
		t.Errorf("messages.deleteHistory: pts_count %d, offset %d", a.GetPtsCount(), a.GetOffset())
	}
	if got := historyTexts(ctx, t, alice, toBob); len(got) != 0 {
		t.Errorf("Alice keeps %v", got)
	}
	// Bob's copy goes with an inbox event.
	got := historyTexts(ctx, t, bob, toAlice)
	for deadline := time.Now().Add(30 * time.Second); len(got) != 0 && time.Now().Before(deadline); got = historyTexts(ctx, t, bob, toAlice) {
		time.Sleep(200 * time.Millisecond)
	}
	if len(got) != 0 {
		t.Errorf("Bob keeps %v", got)
	}
}

// The sub-chat of Saved Messages with the messages forwarded from Bob goes
// with messages.deleteSavedHistory, and leaves the list of Saved Messages
// dialogs (task 020).
func TestDeleteSavedHistory(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	text := fmt.Sprintf("keep this %d", randomID())
	sendMessageID(ctx, t, bob, peerOf(ctx, t, bob, alice), text, randomID())
	toBob := peerOf(ctx, t, alice, bob)
	var id int32
	for deadline := time.Now().Add(30 * time.Second); id == 0 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        toBob,
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
		t.Fatal("Bob's message did not reach Alice")
	}
	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    toBob,
		Id:          []int32{id},
		RandomId:    []int64{randomID()},
		ToPeer:      self,
	}); err != nil {
		t.Fatalf("messages.forwardMessages: %v", err)
	}
	if !savedDialogWith(ctx, t, alice, bob.self.GetId()) {
		t.Fatal("Saved Messages has no sub-chat of Bob after the forward")
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteSavedHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteSavedHistory),
		Peer:        toBob,
	})
	if err != nil {
		t.Fatalf("messages.deleteSavedHistory: %v", err)
	}
	if a := o.(interface {
		To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
	}).To_Messages_AffectedHistory(); a.GetPtsCount() != 1 || a.GetOffset() != 0 {
		t.Errorf("messages.deleteSavedHistory: pts_count %d, offset %d", a.GetPtsCount(), a.GetOffset())
	}
	if savedDialogWith(ctx, t, alice, bob.self.GetId()) {
		t.Error("the sub-chat of Bob stays in Saved Messages")
	}
	// The chat with Bob keeps his message.
	if got := historyTexts(ctx, t, alice, toBob); len(got) != 1 || got[0] != text {
		t.Errorf("the chat with Bob: %v", got)
	}
}

// savedDialogWith tells whether u's Saved Messages has the sub-chat of the
// user peerId (messages.getSavedDialogs).
func savedDialogWith(ctx context.Context, t *testing.T, u *user, peerId int64) bool {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetSavedDialogs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getSavedDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("messages.getSavedDialogs: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_SavedDialogs() *mtproto.Messages_SavedDialogs
	})
	if !ok {
		t.Fatalf("messages.getSavedDialogs returned %T", o)
	}
	for _, d := range r.To_Messages_SavedDialogs().GetDialogs() {
		if d.GetPeer().GetUserId() == peerId {
			return true
		}
	}
	return false
}
