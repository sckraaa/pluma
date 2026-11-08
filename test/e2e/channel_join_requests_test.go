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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestChannelJoinRequests (task 041): Alice exports a link that needs
// approval; Bob's import is INVITE_REQUEST_SENT and Alice learns of it by
// updatePendingJoinRequests, the channel's full data naming him; she
// approves him, the update of the requests is empty and Bob gets the
// channel and messageActionChatJoinedByRequest; she declines Carol, who
// stays out; a second approval is not an error; her link shows a use;
// getChatInviteImporters lists them; toggleJoinRequest on a public group
// makes joinChannel ask.
func TestChannelJoinRequests(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "requests", true)
	peer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesExportChatInvite{
		Constructor:   alice.c.Constructor(mtproto.Predicate_messages_exportChatInvite),
		RequestNeeded: true,
		Peer:          peer,
		Title:         wrapperspb.String("join"),
	})
	if err != nil {
		t.Fatalf("messages.exportChatInvite: %v", err)
	}
	link := o.(interface {
		To_ExportedChatInvite() *mtproto.ExportedChatInvite
	}).To_ExportedChatInvite()
	if !link.GetRequestNeeded() || link.GetTitle().GetValue() != "join" {
		t.Fatalf("the link %v", link)
	}
	hash := link.GetLink()[strings.LastIndex(link.GetLink(), "/+")+2:]
	if !checkInvite(ctx, t, bob, hash).GetRequestNeeded() {
		t.Errorf("checkChatInvite: no request_needed")
	}

	join := func(u *user) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hash,
		})
		return err
	}
	if err = join(bob); !mtclient.IsRPCError(err, "INVITE_REQUEST_SENT") {
		t.Fatalf("Bob's import: %v", err)
	}
	if _, err = alice.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updatePendingJoinRequests && u.GetRequestsPending() == 1 && len(u.GetRecentRequesters()) == 1 {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Alice's update: %v", err)
	}
	input := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{Constructor: alice.c.Constructor(mtproto.Predicate_channels_getFullChannel), Channel: input})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	full := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull()
	if full.GetFullChat().GetRequestsPending().GetValue() != 1 || len(full.GetFullChat().GetRecentRequesters()) != 1 {
		t.Errorf("channelFull %v", full.GetFullChat())
	}

	importers := func(u *user, requested bool, withLink string) *mtproto.Messages_ChatInviteImporters {
		req := &mtproto.TLMessagesGetChatInviteImporters{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getChatInviteImporters),
			Requested:   requested,
			Peer:        peer,
			OffsetUser:  &mtproto.InputUser{Constructor: u.c.Constructor(mtproto.Predicate_inputUserEmpty)},
			Limit:       10,
		}
		if withLink != "" {
			req.Link = wrapperspb.String(withLink)
		}
		o, err := u.c.Invoke(ctx, req)
		if err != nil {
			t.Fatalf("messages.getChatInviteImporters: %v", err)
		}
		return o.(interface {
			To_Messages_ChatInviteImporters() *mtproto.Messages_ChatInviteImporters
		}).To_Messages_ChatInviteImporters()
	}
	if imp := importers(alice, true, ""); imp.GetCount() != 1 || imp.GetImporters()[0].GetUserId() != bob.self.GetId() {
		t.Errorf("requested %v", imp)
	}

	hide := func(self *user, target *user, approved bool) *mtproto.Updates {
		o, err := self.c.Invoke(ctx, &mtproto.TLMessagesHideChatJoinRequest{
			Constructor: self.c.Constructor(mtproto.Predicate_messages_hideChatJoinRequest),
			Approved:    approved,
			Peer:        peer,
			UserId:      inputUserOf(ctx, t, self, target),
		})
		if err != nil {
			t.Fatalf("messages.hideChatJoinRequest: %v", err)
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	}

	// Approved: Alice's update has none left; Bob sees the channel and the
	// service message from himself.
	r := hide(alice, bob, true)
	found := false
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updatePendingJoinRequests && u.GetRequestsPending() == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("the answer %v", r.GetUpdates())
	}
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateChannel && u.GetChannelId() == ch.GetId() {
				return true
			}
		}
		return false
	}); err != nil {
		t.Errorf("Bob's updateChannel: %v", err)
	}
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage &&
				u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionChatJoinedByRequest {
				return true
			}
		}
		return false
	}); err != nil {
		t.Errorf("Bob's service message: %v", err)
	}

	// A second approval is no error and changes nothing.
	r = hide(alice, bob, true)
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updatePendingJoinRequests {
		t.Errorf("a second approval: %v", r.GetUpdates())
	}

	// Carol asks and is declined; the link shows Bob.
	if err = join(carol); !mtclient.IsRPCError(err, "INVITE_REQUEST_SENT") {
		t.Fatalf("Carol's import: %v", err)
	}
	hide(alice, carol, false)
	if err = join(carol); !mtclient.IsRPCError(err, "INVITE_REQUEST_SENT") {
		t.Errorf("Carol may ask again: %v", err)
	}
	if imp := importers(alice, false, link.GetLink()); imp.GetCount() != 1 || imp.GetImporters()[0].GetUserId() != bob.self.GetId() {
		t.Errorf("joined %v", imp)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetExportedChatInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getExportedChatInvite),
		Peer:        peer,
		Link:        link.GetLink(),
	})
	if err != nil {
		t.Fatalf("messages.getExportedChatInvite: %v", err)
	}
	got := o.(interface {
		To_Messages_ExportedChatInvite() *mtproto.Messages_ExportedChatInvite
	}).To_Messages_ExportedChatInvite().GetInvite()
	if got.GetUsage().GetValue() != 1 || got.GetRequested().GetValue() != 1 {
		t.Errorf("the link: usage %v, requested %v", got.GetUsage(), got.GetRequested())
	}
	_, err = alice.c.Invoke(ctx, &mtproto.TLMessagesHideAllChatJoinRequests{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_hideAllChatJoinRequests),
		Approved:    true,
		Peer:        peer,
		Link:        wrapperspb.String(link.GetLink()),
	})
	if err != nil {
		t.Fatalf("messages.hideAllChatJoinRequests: %v", err)
	}
	if _, err = carol.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: carol.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     &mtproto.InputChannel{Constructor: carol.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()},
	}); err != nil {
		t.Errorf("Carol is a member after approve all: %v", err)
	}

	// A group with join_request asks on channels.joinChannel when public.
	_, pub := createChannel(ctx, t, alice, "public requests", true)
	inputPub := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: pub.GetId(), AccessHash: pub.GetAccessHash_FLAGINT64().GetValue()}
	name := "jr" + strings.ToLower(strings.ReplaceAll(phones[0], "+", ""))
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername), Channel: inputPub, Username: name}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsToggleJoinRequest{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleJoinRequest),
		Channel:     inputPub,
		Enabled:     mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("channels.toggleJoinRequest: %v", err)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel7F6A1E22{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_joinChannel7F6A1E22),
		Channel:     &mtproto.InputChannel{Constructor: bob.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: pub.GetId(), AccessHash: pub.GetAccessHash_FLAGINT64().GetValue()},
	}); !mtclient.IsRPCError(err, "INVITE_REQUEST_SENT") {
		t.Errorf("joinChannel of a group asking for approval: %v", err)
	}
}

// TestLeftChannels (task 041): channels.getLeftChannels is of the takeout
// API; called wrapped it names the channel Bob left.
func TestLeftChannels(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "left", true)
	input := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	hash := channelInviteHash(ctx, t, alice, input)
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        hash,
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	bobInput := &mtproto.InputChannel{Constructor: bob.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsLeaveChannel{Constructor: bob.c.Constructor(mtproto.Predicate_channels_leaveChannel), Channel: bobInput}); err != nil {
		t.Fatalf("channels.leaveChannel: %v", err)
	}

	// Not wrapped: TAKEOUT_REQUIRED.
	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetLeftChannels{Constructor: bob.c.Constructor(mtproto.Predicate_channels_getLeftChannels)}); !mtclient.IsRPCError(err, "TAKEOUT_REQUIRED") {
		t.Errorf("not wrapped: %v", err)
	}
	inner := &mtproto.TLChannelsGetLeftChannels{Constructor: bob.c.Constructor(mtproto.Predicate_channels_getLeftChannels)}
	x := mtproto.NewEncodeBuf(32)
	if err := inner.Encode(x, bob.c.Layer()); err != nil {
		t.Fatal(err)
	}
	raw, err := bob.c.InvokeRaw(ctx, &mtproto.TLInvokeWithTakeout{
		Constructor: bob.c.Constructor(mtproto.Predicate_invokeWithTakeout),
		TakeoutId:   randomID() | 1,
		Query:       x.GetBuf(),
	})
	if err != nil {
		t.Fatalf("invokeWithTakeout: %v", err)
	}
	o := mtproto.NewDecodeBuf(raw).Object()
	chats := o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	}).To_Messages_Chats()
	if len(chats.GetChats()) != 1 || chats.GetChats()[0].GetId() != ch.GetId() {
		t.Errorf("left channels: %v", chats.GetChats())
	}
}
