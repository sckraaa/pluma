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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// "Read all mentions" in a basic group: the mention Bob sent is counted in
// Alice's dialog and listed by messages.getUnreadMentions;
// messages.readMentions reads it, Alice's other devices get
// updateReadMessagesContents, and the dialog has no unread mentions after
// (task 020).
func TestReadMentions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	chatId := createChat(ctx, t, bob, fmt.Sprintf("mentions %d", randomID()), alice)

	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        chatPeer(bob, chatId),
		Message:     "hi Alice",
		RandomId:    randomID(),
		Entities: []*mtproto.MessageEntity{
			mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{Offset: 3, Length: 5, UserId_INPUTUSER: inputUserOf(ctx, t, bob, alice)}).To_MessageEntity(),
		},
	}); err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	// Alice's copy arrives with an inbox event after Bob's reply.
	toChat := chatPeer(alice, chatId)
	n := unreadMentions(ctx, t, alice, toChat)
	for deadline := time.Now().Add(30 * time.Second); n == 0 && time.Now().Before(deadline); n = unreadMentions(ctx, t, alice, toChat) {
		time.Sleep(200 * time.Millisecond)
	}
	if n != 1 {
		t.Fatalf("unread_mentions_count %d before reading, want 1", n)
	}
	mentions := unreadMentionIds(ctx, t, alice, toChat)
	if len(mentions) != 1 {
		t.Fatalf("messages.getUnreadMentions: %v", mentions)
	}

	st := getState(ctx, t, alice)
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesReadMentions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_readMentions),
		Peer:        toChat,
	})
	if err != nil {
		t.Fatalf("messages.readMentions: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
	})
	if !ok {
		t.Fatalf("messages.readMentions returned %T", o)
	}
	if a := r.To_Messages_AffectedHistory(); a.GetPtsCount() != 1 || a.GetOffset() != 0 || a.GetPts() != st.GetPts()+1 {
		t.Fatalf("messages.readMentions: pts %d, pts_count %d, offset %d; state pts %d", a.GetPts(), a.GetPtsCount(), a.GetOffset(), st.GetPts())
	}

	var read []int32
	for _, u := range differenceOnce(ctx, t, alice, st).GetOtherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateReadMessagesContents {
			read = append(read, u.GetMessages()...)
		}
	}
	if !slices.Equal(read, mentions) {
		t.Errorf("updateReadMessagesContents of %v, want %v", read, mentions)
	}
	if n := unreadMentions(ctx, t, alice, toChat); n != 0 {
		t.Errorf("unread_mentions_count %d after reading", n)
	}
	if ids := unreadMentionIds(ctx, t, alice, toChat); len(ids) != 0 {
		t.Errorf("messages.getUnreadMentions after reading: %v", ids)
	}
}

// unreadMentions is the unread_mentions_count of u's dialog with peer.
func unreadMentions(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int32 {
	t.Helper()
	d := peerDialogs(ctx, t, u, peer).GetDialogs()
	if len(d) != 1 {
		t.Fatalf("messages.getPeerDialogs: %d dialogs", len(d))
	}
	return d[0].GetUnreadMentionsCount()
}

// unreadMentionIds are the ids messages.getUnreadMentions lists for u's
// dialog with peer.
func unreadMentionIds(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) []int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetUnreadMentions{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getUnreadMentions),
		Peer:        peer,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("messages.getUnreadMentions: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.getUnreadMentions returned %T", o)
	}
	var ids []int32
	for _, m := range r.To_Messages_Messages().GetMessages() {
		ids = append(ids, m.GetId())
	}
	return ids
}
