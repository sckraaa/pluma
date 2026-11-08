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

const scheduleWhenOnline = int32(0x7FFFFFFE)

func scheduleText(ctx context.Context, u *user, peer *mtproto.InputPeer, text string, date int32) (*mtproto.Updates, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor:  u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:         peer,
		Message:      text,
		RandomId:     randomID(),
		ScheduleDate: wrapperspb.Int32(date),
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

// scheduledId is the queue id of the one message a schedule answers with.
func scheduledId(t *testing.T, r *mtproto.Updates) int32 {
	t.Helper()
	var id int32
	found := false
	for _, x := range r.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateNewScheduledMessage {
			id, found = x.GetMessage_MESSAGE().GetId(), true
		}
	}
	if !found {
		t.Fatalf("a schedule's answer without updateNewScheduledMessage: %v", r.GetUpdates())
	}
	return id
}

func scheduledHistory(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, hash int64) *mtproto.Messages_Messages {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetScheduledHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getScheduledHistory),
		Peer:        peer,
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.getScheduledHistory: %v", err)
	}
	return o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages()
}

func updateMatching(ctx context.Context, u *user, name string, wait time.Duration) (*mtproto.Updates, *mtproto.Update, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var hit *mtproto.Update
	r, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == name {
				hit = x
				return true
			}
		}
		return false
	})
	return r, hit, err
}

func containsMessage(u *mtproto.Update, text string) bool {
	return u.GetMessage_MESSAGE().GetMessage() == text
}

// A message waits in the queue, is read, edited and sent by hand.
func TestScheduledPrivate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	peer := inputPeerOf(ctx, t, alice, bob)

	date := int32(time.Now().Unix() + 3600)
	r, err := scheduleText(ctx, alice, peer, "later", date)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateMessageID {
		t.Fatalf("no updateMessageID first: %v", r.GetUpdates())
	}
	id := scheduledId(t, r)

	h := scheduledHistory(ctx, t, alice, peer, 0)
	if len(h.GetMessages()) != 1 || h.GetMessages()[0].GetId() != id || h.GetMessages()[0].GetDate() != date || !h.GetMessages()[0].GetOut() {
		t.Fatalf("the queue: %v", h.GetMessages())
	}
	var hash uint64
	update := func(v int64) { hash ^= hash >> 21; hash ^= hash << 35; hash ^= hash >> 4; hash += uint64(v) }
	update(int64(id))
	update(0)
	update(int64(date))
	if nm := scheduledHistory(ctx, t, alice, peer, int64(hash)); nm.GetPredicateName() != mtproto.Predicate_messages_messagesNotModified {
		t.Errorf("the same hash: %v", nm)
	}
	full, _ := fullUser(ctx, t, alice, inputUserOf(ctx, t, alice, bob))
	if !full.GetHasScheduled() {
		t.Errorf("has_scheduled is off: %v", full)
	}

	if o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:         peer,
		Id:           id,
		Message:      wrapperspb.String("sooner"),
		ScheduleDate: wrapperspb.Int32(date - 60),
	}); err != nil {
		t.Fatalf("edit: %v", err)
	} else if u := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(); u.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateNewScheduledMessage ||
		u.GetUpdates()[0].GetMessage_MESSAGE().GetMessage() != "sooner" || u.GetUpdates()[0].GetMessage_MESSAGE().GetDate() != date-60 {
		t.Fatalf("edit: %v", u.GetUpdates())
	}

	// Sent by hand: out of the queue, the real id beside the queue id.
	getState(ctx, t, alice)
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendScheduledMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendScheduledMessages),
		Peer:        peer,
		Id:          []int32{id},
	})
	if err != nil {
		t.Fatalf("send now: %v", err)
	}
	sent := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	var (
		realId  int32
		deleted bool
	)
	for _, x := range sent.GetUpdates() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage:
			realId = x.GetMessage_MESSAGE().GetId()
		case mtproto.Predicate_updateDeleteScheduledMessages:
			deleted = len(x.GetSentMessages()) == 1 && x.GetMessages()[0] == id
		}
	}
	if realId == 0 || !deleted {
		t.Fatalf("send now: %v", sent.GetUpdates())
	}
	if _, hit, err := updateMatching(ctx, bob, mtproto.Predicate_updateNewMessage, 20*time.Second); err != nil || hit.GetMessage_MESSAGE().GetFromScheduled() || !containsMessage(hit, "sooner") {
		t.Errorf("Bob: %v, %v", hit, err)
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendScheduledMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendScheduledMessages),
		Peer:        peer,
		Id:          []int32{id},
	}); !mtclient.IsRPCError(err, "MESSAGE_ID_INVALID") {
		t.Errorf("sent twice: %v", err)
	}
}

// The limits on the dates: too far ahead, a negative date; and send when
// online outside a private chat.
func TestScheduledDates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	peer := inputPeerOf(ctx, t, alice, bob)
	now := int32(time.Now().Unix())

	if _, err := scheduleText(ctx, alice, peer, "x", now+370*86400); !mtclient.IsRPCError(err, "SCHEDULE_DATE_TOO_LATE") {
		t.Errorf("too far: %v", err)
	}
	if _, err := scheduleText(ctx, alice, peer, "x", -5); !mtclient.IsRPCError(err, "SCHEDULE_DATE_INVALID") {
		t.Errorf("negative: %v", err)
	}
	// A date near enough goes at once, as a plain message.
	r, err := scheduleText(ctx, alice, peer, "at once", now+2)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range r.GetUpdates() {
		found = found || x.GetPredicateName() == mtproto.Predicate_updateNewMessage
	}
	if !found {
		t.Errorf("a near date: %v", r.GetUpdates())
	}
	_, gr := createChannel(ctx, t, alice, "dates", true)
	grPeer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: gr.GetAccessHash_FLAGINT64().GetValue()}
	if _, err := scheduleText(ctx, alice, grPeer, "x", scheduleWhenOnline); !mtclient.IsRPCError(err, "SCHEDULE_DATE_INVALID") {
		t.Errorf("online in a group: %v", err)
	}
}

// At its time a message goes out with from_scheduled for the sender and the
// queue tells both ids; the recipient gets a plain message.
func TestScheduledDelivery(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	peer := inputPeerOf(ctx, t, alice, bob)

	r, err := scheduleText(ctx, alice, peer, "on time", int32(time.Now().Unix()+11))
	if err != nil {
		t.Fatal(err)
	}
	id := scheduledId(t, r)
	if _, hit, err := updateMatching(ctx, alice, mtproto.Predicate_updateNewMessage, 40*time.Second); err != nil || !hit.GetMessage_MESSAGE().GetFromScheduled() {
		t.Fatalf("the sender's update: %v, %v", hit, err)
	}
	if _, hit, err := updateMatching(ctx, alice, mtproto.Predicate_updateDeleteScheduledMessages, 20*time.Second); err != nil || len(hit.GetMessages()) != 1 || hit.GetMessages()[0] != id || len(hit.GetSentMessages()) != 1 {
		t.Fatalf("updateDeleteScheduledMessages: %v, %v", hit, err)
	}
	if _, hit, err := updateMatching(ctx, bob, mtproto.Predicate_updateNewMessage, 20*time.Second); err != nil || hit.GetMessage_MESSAGE().GetFromScheduled() || !containsMessage(hit, "on time") {
		t.Fatalf("the recipient's update: %v, %v", hit, err)
	}
	if h := scheduledHistory(ctx, t, alice, peer, 0); len(h.GetMessages()) != 0 {
		t.Errorf("the queue is not empty: %v", h.GetMessages())
	}
}

// Send when online waits for the peer, and goes at once when it is online.
func TestScheduledWhenOnline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)
	peer := inputPeerOf(ctx, t, alice, bob)

	updateStatus(ctx, t, bob, false)
	waitStatus(ctx, t, alice, bob, mtproto.Predicate_userStatusOnline)
	r, err := scheduleText(ctx, alice, peer, "you are here", scheduleWhenOnline)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range r.GetUpdates() {
		found = found || x.GetPredicateName() == mtproto.Predicate_updateNewMessage
	}
	if !found {
		t.Fatalf("to an online peer: %v", r.GetUpdates())
	}

	updateStatus(ctx, t, bob, true)
	waitStatus(ctx, t, alice, bob, mtproto.Predicate_userStatusOffline)
	r, err = scheduleText(ctx, alice, peer, "wait for me", scheduleWhenOnline)
	if err != nil {
		t.Fatal(err)
	}
	id := scheduledId(t, r)
	h := scheduledHistory(ctx, t, alice, peer, 0)
	if len(h.GetMessages()) != 1 || h.GetMessages()[0].GetDate() != scheduleWhenOnline {
		t.Fatalf("the queue: %v", h.GetMessages())
	}
	updateStatus(ctx, t, bob, false)
	if _, hit, err := updateMatching(ctx, alice, mtproto.Predicate_updateDeleteScheduledMessages, 40*time.Second); err != nil || hit.GetMessages()[0] != id {
		t.Fatalf("when Bob came online: %v, %v", hit, err)
	}

	// A last-seen hidden by the peer: nothing is scheduled.
	mustSetPrivacy(ctx, t, carol, mtproto.Predicate_inputPrivacyKeyStatusTimestamp, ruleOf(carol, mtproto.Predicate_inputPrivacyValueDisallowAll))
	if _, err := scheduleText(ctx, alice, inputPeerOf(ctx, t, alice, carol), "x", scheduleWhenOnline); !mtclient.IsRPCError(err, "SCHEDULE_STATUS_PRIVATE") {
		t.Errorf("a hidden status: %v", err)
	}
}

// The posts of a channel wait in one queue, for those who may post; one
// admin sees another's and needs edit_messages to change them. A member
// does not see the queue.
func TestScheduledChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	_, ch := createChannel(ctx, t, alice, "scheduled", false)
	inputOf := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	peerOfChannel := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	hash := channelInviteHash(ctx, t, alice, inputOf(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hash,
		}); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsEditAdmin{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_editAdmin),
		Channel:     inputOf(alice),
		UserId:      inputUserOf(ctx, t, alice, bob),
		AdminRights: &mtproto.ChatAdminRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatAdminRights), PostMessages: true},
	}); err != nil {
		t.Fatalf("channels.editAdmin: %v", err)
	}
	getState(ctx, t, bob)
	getState(ctx, t, carol)

	r, err := scheduleText(ctx, alice, peerOfChannel(alice), "post", int32(time.Now().Unix()+12))
	if err != nil {
		t.Fatal(err)
	}
	id := scheduledId(t, r)

	// Bob gets the queue's update, sees the post and it is not his.
	if _, hit, err := updateMatching(ctx, bob, mtproto.Predicate_updateNewScheduledMessage, 20*time.Second); err != nil || hit.GetMessage_MESSAGE().GetId() != id {
		t.Fatalf("Bob's update: %v, %v", hit, err)
	}
	h := scheduledHistory(ctx, t, bob, peerOfChannel(bob), 0)
	if len(h.GetMessages()) != 1 || h.GetMessages()[0].GetOut() || !h.GetMessages()[0].GetPost() {
		t.Fatalf("the shared queue: %v", h.GetMessages())
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor:  bob.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:         peerOfChannel(bob),
		Id:           id,
		Message:      wrapperspb.String("not yours"),
		ScheduleDate: wrapperspb.Int32(int32(time.Now().Unix() + 3600)),
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("an admin edits another's post: %v", err)
	}
	if _, err := carol.c.Invoke(ctx, &mtproto.TLMessagesGetScheduledHistory{
		Constructor: carol.c.Constructor(mtproto.Predicate_messages_getScheduledHistory),
		Peer:        peerOfChannel(carol),
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member reads the queue: %v", err)
	}

	if _, hit, err := updateMatching(ctx, carol, mtproto.Predicate_updateNewChannelMessage, 40*time.Second); err != nil || !containsMessage(hit, "post") || hit.GetMessage_MESSAGE().GetFromScheduled() {
		t.Fatalf("the post: %v, %v", hit, err)
	}
	if _, hit, err := updateMatching(ctx, bob, mtproto.Predicate_updateDeleteScheduledMessages, 20*time.Second); err != nil || hit.GetMessages()[0] != id {
		t.Fatalf("Bob: queue update %v, %v", hit, err)
	}
}

// waitStatus waits for viewer to see u with the status, written after
// account.updateStatus answers.
func waitStatus(ctx context.Context, t *testing.T, viewer, u *user, want string) {
	t.Helper()
	input := inputUserOf(ctx, t, viewer, u)
	var got *mtproto.UserStatus
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		_, full := fullUser(ctx, t, viewer, input)
		if got = full.GetStatus(); got.GetPredicateName() == want {
			return
		}
	}
	t.Fatalf("status %v, want %s", got, want)
}
