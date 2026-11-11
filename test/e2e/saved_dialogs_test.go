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
)

// A post saved from a channel makes a sub-chat of Saved Messages with the
// channel, next to "My notes" (task 079): the lists, the sub-chat by id and
// its history carry the channel in chats, the sub-chat is pinned and
// ordered among the pins, and messages.deleteSavedHistory takes it away.
func TestSavedDialogsOfChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	_, ch := createChannel(ctx, t, alice, "saved", false)
	toChannel := &mtproto.InputPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
	}
	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	postText := fmt.Sprintf("a post %d", randomID())
	post := sendMessageID(ctx, t, alice, toChannel, postText, randomID())
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    toChannel,
		Id:          []int32{post},
		RandomId:    []int64{randomID()},
		ToPeer:      self,
	}); err != nil {
		t.Fatalf("messages.forwardMessages: %v", err)
	}
	noteText := fmt.Sprintf("a note %d", randomID())
	sendMessageID(ctx, t, alice, self, noteText, randomID())
	me := alice.self.GetId()

	r := savedDialogs(ctx, t, alice)
	if !hasSavedPeer(r.GetDialogs(), ch.GetId(), 0) || !hasSavedPeer(r.GetDialogs(), 0, me) {
		t.Fatalf("messages.getSavedDialogs: %v", r.GetDialogs())
	}
	if !hasChat(r.GetChats(), ch.GetId()) {
		t.Errorf("messages.getSavedDialogs: no channel in chats %v", r.GetChats())
	}

	// The history of each sub-chat holds its own message only.
	if got := savedHistory(ctx, t, alice, toChannel); len(got.GetMessages()) != 1 || got.GetMessages()[0].GetMessage() != postText || !hasChat(got.GetChats(), ch.GetId()) {
		t.Errorf("messages.getSavedHistory of the channel: %v, chats %v", got.GetMessages(), got.GetChats())
	}
	if got := savedHistory(ctx, t, alice, self); len(got.GetMessages()) != 1 || got.GetMessages()[0].GetMessage() != noteText {
		t.Errorf("messages.getSavedHistory of My notes: %v", got.GetMessages())
	}

	// By id, in the order asked.
	byId := savedDialogsByID(ctx, t, alice, toChannel, self)
	if d := byId.GetDialogs(); len(d) != 2 || d[0].GetPeer().GetChannelId() != ch.GetId() || d[1].GetPeer().GetUserId() != me {
		t.Errorf("messages.getSavedDialogsByID: %v", d)
	}
	if !hasChat(byId.GetChats(), ch.GetId()) || len(byId.GetMessages()) != 2 {
		t.Errorf("messages.getSavedDialogsByID: chats %v, messages %d", byId.GetChats(), len(byId.GetMessages()))
	}

	// Pinned, then put below My notes.
	for _, p := range []*mtproto.InputPeer{toChannel, self} {
		if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesToggleSavedDialogPin{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_toggleSavedDialogPin),
			Pinned:      true,
			Peer:        &mtproto.InputDialogPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputDialogPeer), Peer: p},
		}); err != nil {
			t.Fatalf("messages.toggleSavedDialogPin: %v", err)
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesReorderPinnedSavedDialogs{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_reorderPinnedSavedDialogs),
		Force:       true,
		Order: []*mtproto.InputDialogPeer{
			{Constructor: alice.c.Constructor(mtproto.Predicate_inputDialogPeer), Peer: self},
			{Constructor: alice.c.Constructor(mtproto.Predicate_inputDialogPeer), Peer: toChannel},
		},
	}); err != nil {
		t.Fatalf("messages.reorderPinnedSavedDialogs: %v", err)
	}
	pinned := pinnedSavedDialogs(ctx, t, alice)
	if d := pinned.GetDialogs(); len(d) != 2 || d[0].GetPeer().GetUserId() != me || d[1].GetPeer().GetChannelId() != ch.GetId() || !d[1].GetPinned() {
		t.Errorf("messages.getPinnedSavedDialogs: %v", d)
	}
	if !hasChat(pinned.GetChats(), ch.GetId()) {
		t.Errorf("messages.getPinnedSavedDialogs: no channel in chats %v", pinned.GetChats())
	}

	// The sub-chat of the channel goes; the post stays in the channel.
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteSavedHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteSavedHistory),
		Peer:        toChannel,
	})
	if err != nil {
		t.Fatalf("messages.deleteSavedHistory of a channel: %v", err)
	}
	if a := o.(interface {
		To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
	}).To_Messages_AffectedHistory(); a.GetPtsCount() != 1 || a.GetOffset() != 0 {
		t.Errorf("messages.deleteSavedHistory: pts_count %d, offset %d", a.GetPtsCount(), a.GetOffset())
	}
	if d := savedDialogsByID(ctx, t, alice, toChannel).GetDialogs(); len(d) != 0 {
		t.Errorf("messages.getSavedDialogsByID after the deletion: %v", d)
	}
	if r = savedDialogs(ctx, t, alice); hasSavedPeer(r.GetDialogs(), ch.GetId(), 0) || !hasSavedPeer(r.GetDialogs(), 0, me) {
		t.Errorf("messages.getSavedDialogs after the deletion: %v", r.GetDialogs())
	}
	if got := historyTexts(ctx, t, alice, toChannel); len(got) == 0 {
		t.Error("the channel lost its post")
	}
}

func savedDialogs(ctx context.Context, t *testing.T, u *user) *mtproto.Messages_SavedDialogs {
	t.Helper()
	return toSavedDialogs(t, "messages.getSavedDialogs")(u.c.Invoke(ctx, &mtproto.TLMessagesGetSavedDialogs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getSavedDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       50,
	}))
}

func pinnedSavedDialogs(ctx context.Context, t *testing.T, u *user) *mtproto.Messages_SavedDialogs {
	t.Helper()
	return toSavedDialogs(t, "messages.getPinnedSavedDialogs")(u.c.Invoke(ctx, &mtproto.TLMessagesGetPinnedSavedDialogs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getPinnedSavedDialogs),
	}))
}

func savedDialogsByID(ctx context.Context, t *testing.T, u *user, ids ...*mtproto.InputPeer) *mtproto.Messages_SavedDialogs {
	t.Helper()
	return toSavedDialogs(t, "messages.getSavedDialogsByID")(u.c.Invoke(ctx, &mtproto.TLMessagesGetSavedDialogsByID{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getSavedDialogsByID),
		Ids:         ids,
	}))
}

func toSavedDialogs(t *testing.T, method string) func(mtproto.TLObject, error) *mtproto.Messages_SavedDialogs {
	return func(o mtproto.TLObject, err error) *mtproto.Messages_SavedDialogs {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		r, ok := o.(interface {
			To_Messages_SavedDialogs() *mtproto.Messages_SavedDialogs
		})
		if !ok {
			t.Fatalf("%s returned %T", method, o)
		}
		return r.To_Messages_SavedDialogs()
	}
}

func savedHistory(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) *mtproto.Messages_Messages {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetSavedHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getSavedHistory),
		Peer:        peer,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("messages.getSavedHistory: %v", err)
	}
	return o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages()
}

// hasSavedPeer tells whether dialogs have the sub-chat of the channel
// channelId, or of the user userId.
func hasSavedPeer(dialogs []*mtproto.SavedDialog, channelId, userId int64) bool {
	for _, d := range dialogs {
		if (channelId != 0 && d.GetPeer().GetChannelId() == channelId) || (userId != 0 && d.GetPeer().GetUserId() == userId) {
			return true
		}
	}
	return false
}
