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
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestChannelSlowMode (task 045) end to end: Alice sets the slow mode of her
// supergroup, which Bob, a member, may not; Bob learns it from updateChannel
// and the full channel. His second message before the interval is
// SLOWMODE_WAIT_X, code 420, and the full channel gives when he may send; an
// admin and the creator are not limited. Forwarding several messages is
// refused; an album is one send, and goes when the interval ends.
func TestChannelSlowMode(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, gr := createChannel(ctx, t, alice, "slow", true)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: gr.GetAccessHash_FLAGINT64().GetValue()}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: gr.GetAccessHash_FLAGINT64().GetValue()}
	}
	hash := channelInviteHash(ctx, t, alice, input(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hash,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsEditAdmin{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_editAdmin),
		Channel:     input(alice),
		UserId:      inputUserOf(ctx, t, alice, carol),
		AdminRights: &mtproto.ChatAdminRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatAdminRights), DeleteMessages: true},
	}); err != nil {
		t.Fatalf("channels.editAdmin: %v", err)
	}

	toggle := func(u *user, seconds int32) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsToggleSlowMode{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_toggleSlowMode),
			Channel:     input(u),
			Seconds:     seconds,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	send := func(u *user) (int32, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer(u), Message: "slow", RandomId: randomID(),
		})
		if err != nil {
			return 0, err
		}
		for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
				return x.GetMessage_MESSAGE().GetId(), nil
			}
		}
		return 0, errors.New("no message in the answer")
	}
	full := func(u *user) *mtproto.ChatFull {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel),
			Channel:     input(u),
		})
		if err != nil {
			t.Fatalf("channels.getFullChannel: %v", err)
		}
		return o.(interface {
			To_Messages_ChatFull() *mtproto.Messages_ChatFull
		}).To_Messages_ChatFull().GetFullChat()
	}

	if _, err := toggle(bob, 5); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member sets the slow mode: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if _, err := toggle(alice, 7); !mtclient.IsRPCError(err, "SECONDS_INVALID") {
		t.Errorf("7 seconds: %v, want SECONDS_INVALID", err)
	}
	getState(ctx, t, bob)
	r, err := toggle(alice, 5)
	if err != nil {
		t.Fatalf("channels.toggleSlowMode: %v", err)
	}
	if len(r.GetChats()) == 0 || !r.GetChats()[0].GetSlowmodeEnabled() {
		t.Errorf("the answer's channel: %v", r.GetChats())
	}
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, c := range us.GetChats() {
			if c.GetId() == gr.GetId() && c.GetSlowmodeEnabled() {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Bob does not learn the slow mode: %v", err)
	}
	if _, err = toggle(alice, 5); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("the same interval: %v, want CHAT_NOT_MODIFIED", err)
	}
	if f := full(bob); f.GetSlowmodeSeconds().GetValue() != 5 || f.GetSlowmodeNextSendDate() != nil {
		t.Errorf("before Bob's message: seconds %v, next %v", f.GetSlowmodeSeconds(), f.GetSlowmodeNextSendDate())
	}

	if _, err = send(bob); err != nil {
		t.Fatalf("Bob's first message: %v", err)
	}
	sent := time.Now()
	_, err = send(bob)
	var rpc *mtclient.RPCError
	if !errors.As(err, &rpc) || rpc.Code != 420 || !mtclient.IsRPCError(err, "SLOWMODE_WAIT_") {
		t.Fatalf("Bob's second message: %v, want 420 SLOWMODE_WAIT_X", err)
	}
	if f := full(bob); f.GetSlowmodeNextSendDate() == nil || f.GetSlowmodeNextSendDate().GetValue() <= int32(sent.Unix())-1 {
		t.Errorf("next send date %v", f.GetSlowmodeNextSendDate())
	}

	var ids []int32
	for _, u := range []*user{alice, carol, alice, carol} {
		id, err := send(u)
		if err != nil {
			t.Fatalf("an admin is limited: %v", err)
		}
		ids = append(ids, id)
	}
	if f := full(carol); f.GetSlowmodeNextSendDate() != nil {
		t.Errorf("an admin waits until %v", f.GetSlowmodeNextSendDate())
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    peer(bob), ToPeer: peer(bob),
		Id: ids[:2], RandomId: []int64{randomID(), randomID()},
	}); !mtclient.IsRPCError(err, "SLOWMODE_MULTI_MSGS_DISABLED") {
		t.Errorf("forwarding two messages: %v, want SLOWMODE_MULTI_MSGS_DISABLED", err)
	}

	time.Sleep(time.Until(sent.Add(6 * time.Second)))
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMultiMedia{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMultiMedia),
		Peer:        peer(bob),
		MultiMedia: []*mtproto.InputSingleMedia{
			mtproto.MakeTLInputSingleMedia(&mtproto.InputSingleMedia{Media: uploadPhoto(ctx, t, bob), RandomId: randomID(), Message: "one"}).To_InputSingleMedia(),
			mtproto.MakeTLInputSingleMedia(&mtproto.InputSingleMedia{Media: uploadPhoto(ctx, t, bob), RandomId: randomID(), Message: "two"}).To_InputSingleMedia(),
		},
	}); err != nil {
		t.Fatalf("an album after the interval: %v", err)
	}
	if _, err = send(bob); !mtclient.IsRPCError(err, "SLOWMODE_WAIT_") {
		t.Errorf("right after the album: %v, want SLOWMODE_WAIT_X", err)
	}

	if r, err = toggle(alice, 0); err != nil {
		t.Fatalf("slow mode off: %v", err)
	}
	if r.GetChats()[0].GetSlowmodeEnabled() {
		t.Error("slowmode_enabled after turning it off")
	}
	if _, err = send(bob); err != nil {
		t.Errorf("Bob with the slow mode off: %v", err)
	}
}

// TestChannelSignatureProfiles (task 045) end to end: Alice's channel signs
// its posts with links to the authors. Bob, a subscriber, reads a post from
// Alice with her name, with Alice in the users of the push, the history, the
// messages and the difference, so that his client opens her profile. Links
// off: the post names her and is not from her; signatures off: neither.
func TestChannelSignatureProfiles(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, "signed", false)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	me, err := alice.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
		Constructor: alice.c.Constructor(mtproto.Predicate_users_getFullUser),
		Id:          &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)},
	})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	aliceId := me.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	}).To_Users_UserFull().GetFullUser().GetId()

	toggle := func(signatures, profiles bool) *mtproto.Chat {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsToggleSignatures{
			Constructor:       alice.c.Constructor(mtproto.Predicate_channels_toggleSignatures),
			Channel:           input(alice),
			SignaturesEnabled: signatures,
			ProfilesEnabled:   profiles,
		})
		if err != nil {
			t.Fatalf("channels.toggleSignatures: %v", err)
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats()[0]
	}
	post := func(text string) *mtproto.Update {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer(alice), Message: text, RandomId: randomID(),
		})
		if err != nil {
			t.Fatalf("the post: %v", err)
		}
		for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
				return x
			}
		}
		t.Fatal("no post in the answer")
		return nil
	}
	hasAlice := func(users []*mtproto.User) bool {
		for _, u := range users {
			if u.GetId() == aliceId {
				return true
			}
		}
		return false
	}

	getState(ctx, t, bob)
	if c := toggle(true, true); !c.GetSignatures() || !c.GetSignatureProfiles() {
		t.Fatalf("the channel: signatures %v, profiles %v", c.GetSignatures(), c.GetSignatureProfiles())
	}
	const linked = "post with a link"
	u := post(linked)
	m := u.GetMessage_MESSAGE()
	if m.GetFromId().GetUserId() != aliceId || m.GetPostAuthor().GetValue() != "Alice E2E" || !m.GetPost() {
		t.Errorf("the post: from %v, author %v, post %v", m.GetFromId(), m.GetPostAuthor(), m.GetPost())
	}
	pushed, err := bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetMessage_MESSAGE().GetMessage() == linked {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("the push: %v", err)
	}
	if !hasAlice(pushed.GetUsers()) {
		t.Errorf("Alice not in the users of the push: %v", pushed.GetUsers())
	}

	o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer(bob), Limit: 5,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	h := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages()
	if !hasAlice(h.GetUsers()) {
		t.Errorf("Alice not in the users of the history: %v", h.GetUsers())
	}
	if got := h.GetMessages()[0]; got.GetFromId().GetUserId() != aliceId {
		t.Errorf("the post in the history: from %v", got.GetFromId())
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetMessages{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getMessages),
		Channel:     input(bob),
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{mtproto.MakeTLInputMessageID(&mtproto.InputMessage{
			Id: m.GetId(),
		}).To_InputMessage()},
	})
	if err != nil {
		t.Fatalf("channels.getMessages: %v", err)
	}
	if ms := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages(); !hasAlice(ms.GetUsers()) {
		t.Errorf("Alice not in the users of channels.getMessages: %v", ms.GetUsers())
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLUpdatesGetChannelDifference{
		Constructor: bob.c.Constructor(mtproto.Predicate_updates_getChannelDifference),
		Channel:     input(bob),
		Filter:      &mtproto.ChannelMessagesFilter{Constructor: bob.c.Constructor(mtproto.Predicate_channelMessagesFilterEmpty)},
		Pts:         u.GetPts_INT32() - 1,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("updates.getChannelDifference: %v", err)
	}
	if d := o.(interface {
		To_Updates_ChannelDifference() *mtproto.Updates_ChannelDifference
	}).To_Updates_ChannelDifference(); !hasAlice(d.GetUsers()) {
		t.Errorf("Alice not in the users of the difference: %v", d.GetUsers())
	}

	if c := toggle(true, false); !c.GetSignatures() || c.GetSignatureProfiles() {
		t.Errorf("links off: signatures %v, profiles %v", c.GetSignatures(), c.GetSignatureProfiles())
	}
	if m = post("signed only").GetMessage_MESSAGE(); m.GetFromId() != nil || m.GetPostAuthor().GetValue() != "Alice E2E" {
		t.Errorf("links off: from %v, author %v", m.GetFromId(), m.GetPostAuthor())
	}
	forward := func(dropAuthor bool) *mtproto.Message {
		t.Helper()
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_forwardMessages),
			DropAuthor:  dropAuthor,
			FromPeer:    peer(bob), Id: []int32{m.GetId()}, RandomId: []int64{randomID()},
			ToPeer: &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		})
		if err != nil {
			t.Fatalf("messages.forwardMessages (drop_author %v): %v", dropAuthor, err)
		}
		msgs := newMessages(o.(interface{ To_Updates() *mtproto.Updates }).To_Updates())
		if len(msgs) != 1 {
			t.Fatalf("messages.forwardMessages (drop_author %v): %d messages", dropAuthor, len(msgs))
		}
		return msgs[0]
	}
	if f := forward(false); f.GetPostAuthor() != nil || f.GetFwdFrom().GetPostAuthor().GetValue() != "Alice E2E" {
		t.Errorf("a forwarded signed post: author %v, header author %v", f.GetPostAuthor(), f.GetFwdFrom().GetPostAuthor())
	}
	if f := forward(true); f.GetPostAuthor() != nil || f.GetFwdFrom() != nil {
		t.Errorf("a signed post copied without the sender: author %v, header %v", f.GetPostAuthor(), f.GetFwdFrom())
	}
	if c := toggle(false, true); c.GetSignatures() || c.GetSignatureProfiles() {
		t.Errorf("signatures off: signatures %v, profiles %v", c.GetSignatures(), c.GetSignatureProfiles())
	}
	if m = post("unsigned").GetMessage_MESSAGE(); m.GetFromId() != nil || m.GetPostAuthor() != nil {
		t.Errorf("signatures off: from %v, author %v", m.GetFromId(), m.GetPostAuthor())
	}
}
