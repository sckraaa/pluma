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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Edits and deletions in a supergroup (task 043): the author edits its
// message and another member gets the edit by push; one may not edit or
// delete another's message, the creator deletes any; the deletion reaches
// the members and the history; the difference of the channel folds both.
func TestChannelEdit(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "edit", true)
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
	mine := sendMessageID(ctx, t, bob, peer(bob), "bob's", randomID())
	theirs := sendMessageID(ctx, t, alice, peer(alice), "alice's", randomID())

	edit := func(u *user, id int32, text string) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_editMessage),
			Peer:        peer(u),
			Id:          id,
			Message:     wrapperspb.String(text),
		})
		return err
	}
	if err := edit(bob, mine, "bob's, edited"); err != nil {
		t.Fatalf("the author edits: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateEditChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetId() == mine && m.GetMessage() == "bob's, edited" && m.GetEditDate() != nil
	})
	if err := edit(bob, theirs, "not mine"); !mtclient.IsRPCError(err, "MESSAGE_AUTHOR_REQUIRED") {
		t.Errorf("editing another's: %v, want MESSAGE_AUTHOR_REQUIRED", err)
	}

	del := func(u *user, ids ...int32) (*mtproto.Messages_AffectedMessages, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsDeleteMessages{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_deleteMessages),
			Channel:     input(u),
			Id:          ids,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Messages_AffectedMessages() *mtproto.Messages_AffectedMessages
		}).To_Messages_AffectedMessages(), nil
	}
	if _, err := del(bob, theirs); !mtclient.IsRPCError(err, "MESSAGE_DELETE_FORBIDDEN") {
		t.Errorf("deleting another's: %v, want MESSAGE_DELETE_FORBIDDEN", err)
	}
	r, err := del(alice, mine, theirs)
	if err != nil {
		t.Fatalf("the creator deletes: %v", err)
	}
	if r.GetPtsCount() != 2 {
		t.Errorf("affected pts_count %d, want 2", r.GetPtsCount())
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateDeleteChannelMessages, func(u *mtproto.Update) bool {
		return u.GetChannelId() == ch.GetId() && len(u.GetMessages()) == 2 && u.GetPts_INT32() == r.GetPts()
	})

	o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer(bob),
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	for _, m := range o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages() {
		if m.GetId() == mine || m.GetId() == theirs {
			t.Errorf("message %d is still in the history", m.GetId())
		}
	}
	cd, err := channelDifference(ctx, bob, ch, hash, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	var deletes int
	for _, u := range cd.GetOtherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateDeleteChannelMessages {
			deletes++
		}
		if u.GetPredicateName() == mtproto.Predicate_updateEditChannelMessage {
			t.Errorf("the edit of a message deleted later is in the difference")
		}
	}
	for _, m := range cd.GetNewMessages() {
		if m.GetId() == mine || m.GetId() == theirs {
			t.Errorf("deleted message %d is new in the difference", m.GetId())
		}
	}
	if deletes != 1 {
		t.Errorf("%d deletions in the difference, want 1", deletes)
	}
}

// A subscriber forwards a channel's post to Saved Messages (task 043): the
// copy says it comes from the channel, with the post's id and the channel
// it was saved from.
func TestChannelForward(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "forward", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash: channelInviteHash(ctx, t, alice, &mtproto.InputChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	post := sendMessageID(ctx, t, alice, &mtproto.InputPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}, "a post", randomID())

	o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash},
		Id:          []int32{post},
		RandomId:    []int64{randomID()},
		ToPeer:      &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)},
	})
	if err != nil {
		t.Fatalf("messages.forwardMessages from a channel: %v", err)
	}
	var fwd *mtproto.MessageFwdHeader
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil && m.GetMessage() == "a post" {
			fwd = m.GetFwdFrom()
		}
	}
	if fwd.GetFromId().GetChannelId() != ch.GetId() || fwd.GetChannelPost().GetValue() != post || fwd.GetSavedFromPeer().GetChannelId() != ch.GetId() {
		t.Errorf("the forward's header %v", fwd)
	}
}
