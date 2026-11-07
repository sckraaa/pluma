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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestChannelComments (task 048, the owner's reports of 07.11) end to end:
// Alice links her private supergroup to her channel as its discussion
// group; her post gets a copy in the group; Bob, subscribed to the channel
// only, sees the group, opens the post's comments (getDiscussionMessage)
// and comments on the copy without joining it, and the group stays out of
// his dialogs; the post shows one comment by Bob in getMessagesViews;
// getReplies lists it; Alice has it unread until she reads the thread.
func TestChannelComments(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "news", false)
	_, gr := createChannel(ctx, t, alice, "news chat", true)
	inputOf := func(u *user, c *mtproto.Chat) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	peerOfChannel := func(u *user, c *mtproto.Chat) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	// Bob subscribes to the channel only: its subscribers read and write
	// the comments without joining the group (join_to_send off).
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, inputOf(alice, ch)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	link := func(u *user, group *mtproto.InputChannel) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLChannelsSetDiscussionGroup{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_setDiscussionGroup),
			Broadcast:   inputOf(u, ch),
			Group:       group,
		})
		return err
	}
	if err := link(bob, inputOf(alice, gr)); err == nil {
		t.Errorf("a subscriber links: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetGroupsForDiscussion{Constructor: alice.c.Constructor(mtproto.Predicate_channels_getGroupsForDiscussion)})
	if err != nil {
		t.Fatalf("channels.getGroupsForDiscussion: %v", err)
	}
	var offered bool
	for _, c := range o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	}).To_Messages_Chats().GetChats() {
		offered = offered || c.GetId() == gr.GetId()
	}
	if !offered {
		t.Error("the group is not offered for discussion")
	}
	if err = link(alice, inputOf(alice, gr)); err != nil {
		t.Fatalf("channels.setDiscussionGroup: %v", err)
	}
	if err = link(alice, inputOf(alice, gr)); !mtclient.IsRPCError(err, "LINK_NOT_MODIFIED") {
		t.Errorf("the same link again: %v, want LINK_NOT_MODIFIED", err)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOfChannel(alice, ch), Message: "a post to discuss", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("the post: %v", err)
	}
	var post int32
	var commentsOn bool
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateMessageID:
			post = u.GetId_INT32()
		case mtproto.Predicate_updateNewChannelMessage:
			r := u.GetMessage_MESSAGE().GetReplies()
			commentsOn = r.GetComments() && r.GetChannelId().GetValue() == gr.GetId()
		}
	}
	// The post itself says it has comments, as every view of it does: the
	// clients show the button by it, and a view without it (a push, the
	// difference, the dialogs) hid the button (the owner's report of 08.11).
	if !commentsOn {
		t.Error("the post does not say it has comments")
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLUpdatesGetChannelDifference{
		Constructor: bob.c.Constructor(mtproto.Predicate_updates_getChannelDifference),
		Channel:     inputOf(bob, ch),
		Filter:      &mtproto.ChannelMessagesFilter{Constructor: bob.c.Constructor(mtproto.Predicate_channelMessagesFilterEmpty)},
		Pts:         1,
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("updates.getChannelDifference: %v", err)
	}
	var inDifference bool
	for _, m := range o.(interface {
		To_Updates_ChannelDifference() *mtproto.Updates_ChannelDifference
	}).To_Updates_ChannelDifference().GetNewMessages() {
		inDifference = inDifference || m.GetId() == post && m.GetReplies().GetComments()
	}
	if !inDifference {
		t.Error("the post in the difference does not say it has comments")
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetDiscussionMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getDiscussionMessage),
		Peer:        peerOfChannel(bob, ch), MsgId: post,
	})
	if err != nil {
		t.Fatalf("messages.getDiscussionMessage: %v", err)
	}
	dm := o.(interface {
		To_Messages_DiscussionMessage() *mtproto.Messages_DiscussionMessage
	}).To_Messages_DiscussionMessage()
	if len(dm.GetMessages()) != 1 || dm.GetMessages()[0].GetPeerId().GetChannelId() != gr.GetId() ||
		dm.GetMessages()[0].GetFwdFrom().GetChannelPost().GetValue() != post || dm.GetMessages()[0].GetMessage() != "a post to discuss" {
		t.Fatalf("the post's copy: %v", dm.GetMessages())
	}
	copyId := dm.GetMessages()[0].GetId()
	var seen bool
	for _, c := range dm.GetChats() {
		seen = seen || c.GetId() == gr.GetId() && c.GetPredicateName() == mtproto.Predicate_channel
	}
	if !seen {
		t.Errorf("Bob does not see the discussion group: %v", dm.GetChats())
	}
	// The clients ask for the members of the group on opening its comments,
	// and Android and iOS ask again and again on an error, never opening
	// them (the owner's report of 08.11).
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetParticipants{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getParticipants),
		Channel:     inputOf(alice, gr),
		Filter:      &mtproto.ChannelParticipantsFilter{Constructor: bob.c.Constructor(mtproto.Predicate_channelParticipantsRecent)},
		Limit:       50,
	}); err != nil {
		t.Errorf("Bob asks for the group's members: %v", err)
	}

	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOfChannel(bob, gr), Message: "a comment", RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: copyId, TopMsgId: mtproto.MakeFlagsInt32(copyId)}).To_InputReplyTo(),
	}); err != nil {
		t.Fatalf("the comment: %v", err)
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getDialogs: %v", err)
	}
	for _, d := range o.(interface {
		To_Messages_Dialogs() *mtproto.Messages_Dialogs
	}).To_Messages_Dialogs().GetDialogs() {
		if d.GetPeer().GetChannelId() == gr.GetId() {
			t.Error("the group Bob commented in is in his dialogs")
		}
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesViews{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessagesViews),
		Peer:        peerOfChannel(alice, ch), Id: []int32{post}, Increment: mtproto.BoolFalse,
	})
	if err != nil {
		t.Fatalf("messages.getMessagesViews: %v", err)
	}
	views := o.(interface {
		To_Messages_MessageViews() *mtproto.Messages_MessageViews
	}).To_Messages_MessageViews().GetViews()
	if len(views) != 1 {
		t.Fatalf("views %v", views)
	}
	r := views[0].GetReplies()
	if !r.GetComments() || r.GetReplies() != 1 || r.GetChannelId().GetValue() != gr.GetId() ||
		len(r.GetRecentRepliers()) != 1 || r.GetRecentRepliers()[0].GetUserId() != bob.self.GetId() {
		t.Errorf("the post's comments: %v", r)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetReplies{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getReplies),
		Peer:        peerOfChannel(alice, gr), MsgId: copyId, Limit: 10,
	})
	if err != nil {
		t.Fatalf("messages.getReplies: %v", err)
	}
	if m := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(m) != 1 || m[0].GetMessage() != "a comment" {
		t.Errorf("the thread: %v", m)
	}

	unread := func() int32 {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetDiscussionMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getDiscussionMessage),
			Peer:        peerOfChannel(alice, ch), MsgId: post,
		})
		if err != nil {
			t.Fatalf("messages.getDiscussionMessage: %v", err)
		}
		return o.(interface {
			To_Messages_DiscussionMessage() *mtproto.Messages_DiscussionMessage
		}).To_Messages_DiscussionMessage().GetUnreadCount()
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesReadDiscussion{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_readDiscussion),
		Peer:        peerOfChannel(alice, gr), MsgId: copyId, ReadMaxId: copyId + 1,
	}); err != nil {
		t.Errorf("Bob reads the comments: %v", err)
	}
	if n := unread(); n != 1 {
		t.Errorf("Alice's unread comments %d, want 1", n)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesReadDiscussion{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_readDiscussion),
		Peer:        peerOfChannel(alice, gr), MsgId: copyId, ReadMaxId: copyId + 1,
	}); err != nil {
		t.Fatalf("messages.readDiscussion: %v", err)
	}
	if n := unread(); n != 0 {
		t.Errorf("Alice's unread comments after reading %d", n)
	}
}
