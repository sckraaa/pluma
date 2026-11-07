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

// TestChannelComments (task 048, the owner's report of 07.11) end to end:
// Alice links her supergroup to her channel as its discussion group; her
// post gets a copy in the group; Bob opens the post's comments
// (getDiscussionMessage), comments on the copy, and the post shows one
// comment by Bob in getMessagesViews and in the history; getReplies lists
// the comment; Alice has it unread until she reads the thread.
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
	for _, c := range []*mtproto.Chat{ch, gr} {
		if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        channelInviteHash(ctx, t, alice, inputOf(alice, c)),
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	link := func(u *user, group *mtproto.InputChannel) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLChannelsSetDiscussionGroup{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_setDiscussionGroup),
			Broadcast:   inputOf(u, ch),
			Group:       group,
		})
		return err
	}
	if err := link(bob, inputOf(bob, gr)); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
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
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
			post = u.GetId_INT32()
		}
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

	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOfChannel(bob, gr), Message: "a comment", RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: copyId, TopMsgId: mtproto.MakeFlagsInt32(copyId)}).To_InputReplyTo(),
	}); err != nil {
		t.Fatalf("the comment: %v", err)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesViews{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessagesViews),
		Peer:        peerOfChannel(alice, ch), Id: []int32{post},
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
