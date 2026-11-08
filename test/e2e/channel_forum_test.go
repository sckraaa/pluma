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

// TestChannelForum (task 060) end to end as the clients use a forum: Alice
// turns a supergroup into a forum, and its old messages are in General;
// topics are created, Bob receives the new topic, posts in it (top_msg_id)
// and replies within it; history, search and unread counts are per topic;
// closing a topic stops a member from writing to it; a pin, a read and a
// deletion of a topic work.
func TestChannelForum(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "club", true)
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
	old := sendMessageID(ctx, t, alice, peer(alice), "before the forum", randomID())

	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsToggleForum{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_toggleForum),
		Channel:     input(bob), Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse,
	}); err == nil {
		t.Fatal("a member turned the forum on")
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsToggleForum{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleForum),
		Channel:     input(alice), Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse,
	})
	if err != nil {
		t.Fatalf("channels.toggleForum: %v", err)
	}
	if c := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats(); len(c) == 0 || !c[0].GetForum() {
		t.Fatalf("the channel of the answer: %v", c)
	}

	getTopics := func(u *user) *mtproto.Messages_ForumTopics {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetForumTopics{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getForumTopics),
			Peer:        peer(u), Limit: 20,
		})
		if err != nil {
			t.Fatalf("messages.getForumTopics: %v", err)
		}
		return o.(interface {
			To_Messages_ForumTopics() *mtproto.Messages_ForumTopics
		}).To_Messages_ForumTopics()
	}
	topicIn := func(r *mtproto.Messages_ForumTopics, id int32) *mtproto.ForumTopic {
		t.Helper()
		for _, tp := range r.GetTopics() {
			if tp.GetId() == id {
				return tp
			}
		}
		t.Fatalf("topic %d not in %v", id, r.GetTopics())
		return nil
	}
	replies := func(u *user, topic int32) *mtproto.Messages_Messages {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetReplies{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getReplies),
			Peer:        peer(u), MsgId: topic, Limit: 20,
		})
		if err != nil {
			t.Fatalf("messages.getReplies: %v", err)
		}
		return o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages()
	}
	if g := replies(alice, 1); g.GetCount() < 1 || g.GetMessages()[0].GetId() != old {
		t.Fatalf("General after the toggle: %v", g.GetMessages())
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesCreateForumTopic{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_createForumTopic),
		Peer:        peer(alice), Title: "Topic one", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.createForumTopic: %v", err)
	}
	var topic int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			m := u.GetMessage_MESSAGE()
			if m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionTopicCreate || m.GetAction().GetTitle_STRING() != "Topic one" {
				t.Fatalf("the service message: %v", m)
			}
			topic = m.GetId()
		}
	}
	if topic == 0 {
		t.Fatal("no topic in the answer")
	}

	// Bob gets the topic pushed, as a client does after getState.
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage && u.GetMessage_MESSAGE().GetId() == topic {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("the topic is not pushed: %v", err)
	}

	sendReply := func(u *user, text string, reply *mtproto.InputReplyTo) *mtproto.Message {
		t.Helper()
		us, err := sendWithReply(ctx, u, peer(u), text, reply)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		for _, up := range us.GetUpdates() {
			if up.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
				return up.GetMessage_MESSAGE()
			}
		}
		t.Fatalf("no new message: %v", us)
		return nil
	}
	send := func(u *user, text string, replyMsg, top int32) *mtproto.Message {
		t.Helper()
		reply := &mtproto.InputReplyTo{Constructor: u.c.Constructor(mtproto.Predicate_inputReplyToMessage), ReplyToMsgId: replyMsg}
		if top != 0 {
			reply.TopMsgId = mtproto.MakeFlagsInt32(top)
		}
		return sendReply(u, text, reply)
	}
	first := send(bob, "in topic", topic, 0)
	if h := first.GetReplyTo(); !h.GetForumTopic() || h.GetReplyToMsgId_FLAGINT32().GetValue() != topic || h.GetReplyToTopId() != nil {
		t.Errorf("reply_to of a post in the topic: %v", h)
	}
	second := send(alice, "reply in topic", first.GetId(), topic)
	if h := second.GetReplyTo(); !h.GetForumTopic() || h.GetReplyToMsgId_FLAGINT32().GetValue() != first.GetId() || h.GetReplyToTopId().GetValue() != topic {
		t.Errorf("reply_to of a reply in the topic: %v", h)
	}
	general := sendMessageID(ctx, t, bob, peer(bob), "in general", randomID())

	hist := replies(alice, topic)
	if hist.GetCount() != 3 || hist.GetMessages()[hist.GetCount()-1].GetId() != topic {
		t.Fatalf("the topic's history: %v", hist.GetMessages())
	}
	if g := replies(alice, 1); g.GetMessages()[0].GetId() != general {
		t.Errorf("General after posts: %v", g.GetMessages())
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSearch{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_search),
		Peer:        peer(alice), Q: "topic", Filter: &mtproto.MessagesFilter{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)},
		TopMsgId: mtproto.MakeFlagsInt32(topic), Limit: 10,
	})
	if err != nil {
		t.Fatalf("messages.search: %v", err)
	}
	if m := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(m) != 2 {
		t.Errorf("search in the topic: %d, want 2: %v", len(m), m)
	}

	// Bob sent the message before Alice's reply: that reply is unread for him.
	tp := topicIn(getTopics(bob), topic)
	if tp.GetUnreadCount() != 1 || tp.GetTopMessage() != second.GetId() {
		t.Errorf("bob's topic: %v", tp)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesReadDiscussion{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_readDiscussion),
		Peer:        peer(bob), MsgId: topic, ReadMaxId: second.GetId(),
	}); err != nil {
		t.Fatalf("messages.readDiscussion: %v", err)
	}
	if tp = topicIn(getTopics(bob), topic); tp.GetUnreadCount() != 0 {
		t.Errorf("after read: %v", tp)
	}

	// A reply from General to a message of a topic names the forum, as
	// tdesktop and Android send it, and stays in General.
	quoted := sendReply(bob, "from general", &mtproto.InputReplyTo{
		Constructor: bob.c.Constructor(mtproto.Predicate_inputReplyToMessage), ReplyToMsgId: first.GetId(), ReplyToPeerId: peer(bob),
	})
	if h := quoted.GetReplyTo(); h.GetForumTopic() || h.GetReplyToMsgId_FLAGINT32().GetValue() != first.GetId() {
		t.Errorf("reply_to of a reply to another topic: %v", h)
	}
	if g := replies(alice, 1); g.GetMessages()[0].GetId() != quoted.GetId() {
		t.Errorf("General after a reply to another topic: %v", g.GetMessages())
	}

	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesUpdatePinnedForumTopic{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_updatePinnedForumTopic),
		Peer:        peer(bob), TopicId: topic, Pinned: mtproto.BoolTrue,
	}); err == nil {
		t.Error("a member pinned a topic")
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesUpdatePinnedForumTopic{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updatePinnedForumTopic),
		Peer:        peer(alice), TopicId: topic, Pinned: mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("messages.updatePinnedForumTopic: %v", err)
	}
	if !getTopics(alice).GetTopics()[0].GetPinned() {
		t.Error("the pinned topic is not first")
	}

	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesEditForumTopic{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_editForumTopic),
		Peer:        peer(alice), TopicId: topic, Closed: mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("messages.editForumTopic: %v", err)
	}
	reply := &mtproto.InputReplyTo{Constructor: bob.c.Constructor(mtproto.Predicate_inputReplyToMessage), ReplyToMsgId: topic}
	if _, err = sendWithReply(ctx, bob, peer(bob), "too late", reply); err == nil {
		t.Error("a member wrote to a closed topic")
	} else if e, ok := err.(*mtclient.RPCError); !ok || e.Message != "TOPIC_CLOSED" {
		t.Errorf("writing to a closed topic: %v", err)
	}
	if o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteTopicHistory{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteTopicHistory),
		Peer:        peer(alice), TopMsgId: topic,
	}); err != nil {
		t.Fatalf("messages.deleteTopicHistory: %v", err)
	}
	if a := o.(interface {
		To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
	}).To_Messages_AffectedHistory(); a.GetOffset() != 0 || a.GetPtsCount() < 3 {
		t.Errorf("the deletion: %v", a)
	}
	if r := getTopics(alice); len(r.GetTopics()) != 1 || r.GetTopics()[0].GetId() != 1 {
		t.Errorf("topics after the deletion: %v", r.GetTopics())
	}
}
