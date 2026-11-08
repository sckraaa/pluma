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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// commentsFixture is a channel of Alice's with a linked group Bob is in.
type commentsFixture struct {
	ctx        context.Context
	t          *testing.T
	alice, bob *user
	ch, gr     *mtproto.Chat
}

func (f *commentsFixture) peer(u *user, c *mtproto.Chat) *mtproto.InputPeer {
	return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
}

func (f *commentsFixture) input(u *user, c *mtproto.Chat) *mtproto.InputChannel {
	return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
}

func newCommentsFixture(ctx context.Context, t *testing.T) *commentsFixture {
	t.Helper()
	phones := testPhones(t, 2)
	f := &commentsFixture{ctx: ctx, t: t, alice: login(ctx, t, phones[0], "Alice"), bob: login(ctx, t, phones[1], "Bob")}
	_, f.ch = createChannel(ctx, t, f.alice, "news", false)
	_, f.gr = createChannel(ctx, t, f.alice, "news chat", true)
	for _, c := range []*mtproto.Chat{f.ch, f.gr} {
		if _, err := f.bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: f.bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        channelInviteHash(ctx, t, f.alice, f.input(f.alice, c)),
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	if _, err := f.alice.c.Invoke(ctx, &mtproto.TLChannelsSetDiscussionGroup{
		Constructor: f.alice.c.Constructor(mtproto.Predicate_channels_setDiscussionGroup),
		Broadcast:   f.input(f.alice, f.ch),
		Group:       f.input(f.alice, f.gr),
	}); err != nil {
		t.Fatalf("channels.setDiscussionGroup: %v", err)
	}
	getState(ctx, t, f.alice)
	getState(ctx, t, f.bob)
	return f
}

// post publishes a post and returns its id and its copy's.
func (f *commentsFixture) post(text string) (post, copyId int32) {
	f.t.Helper()
	o, err := f.alice.c.Invoke(f.ctx, &mtproto.TLMessagesSendMessage{
		Constructor: f.alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        f.peer(f.alice, f.ch), Message: text, RandomId: randomID(),
	})
	if err != nil {
		f.t.Fatalf("the post: %v", err)
	}
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
			post = u.GetId_INT32()
		}
	}
	dm, err := f.discussion(f.alice, f.ch, post)
	if err != nil {
		f.t.Fatalf("messages.getDiscussionMessage: %v", err)
	}
	if len(dm.GetMessages()) != 1 {
		f.t.Fatalf("the copy: %v", dm.GetMessages())
	}
	return post, dm.GetMessages()[0].GetId()
}

func (f *commentsFixture) discussion(u *user, c *mtproto.Chat, id int32) (*mtproto.Messages_DiscussionMessage, error) {
	o, err := u.c.Invoke(f.ctx, &mtproto.TLMessagesGetDiscussionMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getDiscussionMessage),
		Peer:        f.peer(u, c), MsgId: id,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface {
		To_Messages_DiscussionMessage() *mtproto.Messages_DiscussionMessage
	}).To_Messages_DiscussionMessage(), nil
}

// comment writes a comment of u in the thread of copyId and returns its id.
func (f *commentsFixture) comment(u *user, copyId int32, text string) int32 {
	f.t.Helper()
	o, err := u.c.Invoke(f.ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        f.peer(u, f.gr), Message: text, RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: copyId, TopMsgId: mtproto.MakeFlagsInt32(copyId)}).To_InputReplyTo(),
	})
	if err != nil {
		f.t.Fatalf("the comment: %v", err)
	}
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateMessageID {
			return x.GetId_INT32()
		}
	}
	f.t.Fatal("the comment's answer without its id")
	return 0
}

func (f *commentsFixture) deleteIn(c *mtproto.Chat, ids ...int32) {
	f.t.Helper()
	if _, err := f.alice.c.Invoke(f.ctx, &mtproto.TLChannelsDeleteMessages{
		Constructor: f.alice.c.Constructor(mtproto.Predicate_channels_deleteMessages),
		Channel:     f.input(f.alice, c),
		Id:          ids,
	}); err != nil {
		f.t.Fatalf("channels.deleteMessages: %v", err)
	}
}

// TestChannelPostEditReachesCopy: an edit of a post is an edit of its copy
// too, pushed with the group's pts to its members and to the editor, whose
// answer holds the post alone.
func TestChannelPostEditReachesCopy(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newCommentsFixture(ctx, t)
	post, copyId := f.post("as posted")
	waitUpdate(ctx, t, f.alice, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetPeerId().GetChannelId() == f.gr.GetId() && m.GetId() == copyId
	})
	f.comment(f.bob, copyId, "a comment")

	if _, err := f.alice.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor: f.alice.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        f.peer(f.alice, f.ch), Id: post, Message: wrapperspb.String("as edited"),
	}); err != nil {
		t.Fatalf("messages.editMessage: %v", err)
	}
	isCopy := func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetPeerId().GetChannelId() == f.gr.GetId() && m.GetId() == copyId && m.GetMessage() == "as edited" && m.GetEditDate() != nil && u.GetPts_INT32() != 0
	}
	waitUpdate(ctx, t, f.bob, mtproto.Predicate_updateEditChannelMessage, isCopy)
	waitUpdate(ctx, t, f.bob, mtproto.Predicate_updateEditChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetPeerId().GetChannelId() == f.ch.GetId() && m.GetId() == post && m.GetReplies().GetReplies() == 1
	})
	waitUpdate(ctx, t, f.alice, mtproto.Predicate_updateEditChannelMessage, isCopy)
	dm, err := f.discussion(f.bob, f.ch, post)
	if err != nil {
		t.Fatalf("messages.getDiscussionMessage: %v", err)
	}
	if m := dm.GetMessages(); len(m) != 1 || m[0].GetMessage() != "as edited" {
		t.Errorf("the copy: %v", m)
	}
}

// TestChannelPostDeleteTakesCopy: a deleted post takes its copy, the group
// told by a deletion of its own; comments stay in the group; the post's
// comments do not open; the thread opens by a comment. A copy removed in
// the group turns the comments of its post off.
func TestChannelPostDeleteTakesCopy(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newCommentsFixture(ctx, t)
	post, copyId := f.post("to delete")
	comment := f.comment(f.bob, copyId, "a comment")

	f.deleteIn(f.ch, post)
	waitUpdate(ctx, t, f.bob, mtproto.Predicate_updateDeleteChannelMessages, func(u *mtproto.Update) bool {
		return u.GetChannelId() == f.gr.GetId() && slices.Contains(u.GetMessages(), copyId)
	})
	if _, err := f.discussion(f.bob, f.ch, post); !mtclient.IsRPCError(err, "MSG_ID_INVALID") {
		t.Errorf("the comments of a deleted post: %v, want MSG_ID_INVALID", err)
	}
	dm, err := f.discussion(f.bob, f.gr, comment)
	if err != nil {
		t.Fatalf("the thread by a comment: %v", err)
	}
	if len(dm.GetMessages()) != 0 || dm.GetMaxId().GetValue() != comment {
		t.Errorf("the thread: %v, max %d", dm.GetMessages(), dm.GetMaxId().GetValue())
	}
	o, err := f.bob.c.Invoke(ctx, &mtproto.TLMessagesGetReplies{
		Constructor: f.bob.c.Constructor(mtproto.Predicate_messages_getReplies),
		Peer:        f.peer(f.bob, f.gr), MsgId: copyId, Limit: 10,
	})
	if err != nil {
		t.Fatalf("messages.getReplies: %v", err)
	}
	if m := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(m) != 1 || m[0].GetId() != comment {
		t.Errorf("the comments left: %v", m)
	}
	// Who still has the thread open replies to its deleted start.
	later := f.comment(f.alice, copyId, "after the post")
	dm, err = f.discussion(f.alice, f.gr, later)
	if err != nil || dm.GetMaxId().GetValue() != later {
		t.Errorf("the thread after a later comment: max %d, %v, want %d", dm.GetMaxId().GetValue(), err, later)
	}

	post2, copy2 := f.post("to lose its comments")
	f.deleteIn(f.gr, copy2)
	waitUpdate(ctx, t, f.bob, mtproto.Predicate_updateEditChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetPeerId().GetChannelId() == f.ch.GetId() && m.GetId() == post2 && m.GetReplies() == nil
	})
	if _, err = f.discussion(f.bob, f.ch, post2); !mtclient.IsRPCError(err, "MSG_ID_INVALID") {
		t.Errorf("the comments of a post without its copy: %v, want MSG_ID_INVALID", err)
	}
	o, err = f.alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesViews{
		Constructor: f.alice.c.Constructor(mtproto.Predicate_messages_getMessagesViews),
		Peer:        f.peer(f.alice, f.ch), Id: []int32{post2}, Increment: mtproto.BoolFalse,
	})
	if err != nil {
		t.Fatalf("messages.getMessagesViews: %v", err)
	}
	if v := o.(interface {
		To_Messages_MessageViews() *mtproto.Messages_MessageViews
	}).To_Messages_MessageViews().GetViews(); len(v) != 1 || v[0].GetReplies() != nil {
		t.Errorf("the post's views: %v", v)
	}
}

// TestChannelDiscussionReadUpdates: a read of comments reaches the reader's
// devices with the post of the thread and the authors of what it read, and
// the author sees the mark on opening the thread again.
func TestChannelDiscussionReadUpdates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newCommentsFixture(ctx, t)
	post, copyId := f.post("to read")
	comment := f.comment(f.alice, copyId, "by Alice")

	if _, err := f.bob.c.Invoke(ctx, &mtproto.TLMessagesReadDiscussion{
		Constructor: f.bob.c.Constructor(mtproto.Predicate_messages_readDiscussion),
		Peer:        f.peer(f.bob, f.gr), MsgId: copyId, ReadMaxId: comment,
	}); err != nil {
		t.Fatalf("messages.readDiscussion: %v", err)
	}
	waitUpdate(ctx, t, f.bob, mtproto.Predicate_updateReadChannelDiscussionInbox, func(u *mtproto.Update) bool {
		return u.GetChannelId() == f.gr.GetId() && u.GetTopMsgId_INT32() == copyId && u.GetReadMaxId() == comment &&
			u.GetBroadcastId().GetValue() == f.ch.GetId() && u.GetBroadcastPost().GetValue() == post
	})
	waitUpdate(ctx, t, f.alice, mtproto.Predicate_updateReadChannelDiscussionOutbox, func(u *mtproto.Update) bool {
		return u.GetChannelId() == f.gr.GetId() && u.GetTopMsgId_INT32() == copyId && u.GetReadMaxId() == comment
	})
	dm, err := f.discussion(f.alice, f.ch, post)
	if err != nil {
		t.Fatalf("messages.getDiscussionMessage: %v", err)
	}
	if dm.GetReadOutboxMaxId().GetValue() != comment {
		t.Errorf("read_outbox_max_id %d, want %d", dm.GetReadOutboxMaxId().GetValue(), comment)
	}
	o, err := f.bob.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesViews{
		Constructor: f.bob.c.Constructor(mtproto.Predicate_messages_getMessagesViews),
		Peer:        f.peer(f.bob, f.ch), Id: []int32{post}, Increment: mtproto.BoolFalse,
	})
	if err != nil {
		t.Fatalf("messages.getMessagesViews: %v", err)
	}
	if v := o.(interface {
		To_Messages_MessageViews() *mtproto.Messages_MessageViews
	}).To_Messages_MessageViews().GetViews(); len(v) != 1 || v[0].GetReplies().GetReadMaxId().GetValue() != comment {
		t.Errorf("the post's views: %v, want read_max_id %d", v, comment)
	}
}

// TestChannelDiscussionBasicGroup: a basic group its creator made is
// offered for discussion, not to a member; it is moved to a supergroup and
// linked as the clients do; a supergroup that hides its history is offered
// and refused until it shows it.
func TestChannelDiscussionBasicGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, "news", false)
	chatId := createChat(ctx, t, alice, "basic chat", bob)

	offered := func(u *user, id int64, predicate string) bool {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetGroupsForDiscussion{Constructor: u.c.Constructor(mtproto.Predicate_channels_getGroupsForDiscussion)})
		if err != nil {
			t.Fatalf("channels.getGroupsForDiscussion: %v", err)
		}
		for _, c := range o.(interface {
			To_Messages_Chats() *mtproto.Messages_Chats
		}).To_Messages_Chats().GetChats() {
			if c.GetId() == id && c.GetPredicateName() == predicate {
				return true
			}
		}
		return false
	}
	input := func(c *mtproto.Chat) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	link := func(group *mtproto.Chat) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLChannelsSetDiscussionGroup{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_setDiscussionGroup),
			Broadcast:   input(ch),
			Group:       input(group),
		})
		return err
	}
	if !offered(alice, chatId, mtproto.Predicate_chat) {
		t.Error("the basic group is not offered to its creator")
	}
	if offered(bob, chatId, mtproto.Predicate_chat) {
		t.Error("the basic group is offered to a member, who may not migrate it")
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesMigrateChat{Constructor: alice.c.Constructor(mtproto.Predicate_messages_migrateChat), ChatId: chatId})
	if err != nil {
		t.Fatalf("messages.migrateChat: %v", err)
	}
	var super *mtproto.Chat
	for _, c := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats() {
		if c.GetPredicateName() == mtproto.Predicate_channel {
			super = c
		}
	}
	if super == nil {
		t.Fatal("messages.migrateChat without the supergroup")
	}
	if offered(alice, chatId, mtproto.Predicate_chat) {
		t.Error("the migrated group is still offered")
	}
	if !offered(alice, super.GetId(), mtproto.Predicate_channel) {
		t.Error("the supergroup is not offered")
	}
	if err = link(super); err != nil {
		t.Fatalf("channels.setDiscussionGroup of the migrated group: %v", err)
	}

	_, hidden := createChannel(ctx, t, alice, "hidden chat", true)
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsTogglePreHistoryHidden{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_togglePreHistoryHidden),
		Channel:     input(hidden),
		Enabled:     mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("channels.togglePreHistoryHidden: %v", err)
	}
	if !offered(alice, hidden.GetId(), mtproto.Predicate_channel) {
		t.Error("the group that hides its history is not offered")
	}
	if err = link(hidden); !mtclient.IsRPCError(err, "MEGAGROUP_PREHISTORY_HIDDEN") {
		t.Errorf("linking a group that hides its history: %v, want MEGAGROUP_PREHISTORY_HIDDEN", err)
	}
}
