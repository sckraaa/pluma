//go:build integration

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

package core

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

func (f *diffFixture) forum(creator int64) int64 {
	f.t.Helper()
	id := f.create(creator)
	// A real channel starts with its first message, id 1.
	f.send(id, creator, 1)
	if _, err := f.c.ChannelToggleForum(&channel.TLChannelToggleForum{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse}); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *diffFixture) createTopic(self, id int64, title string, randomId int64) int32 {
	f.t.Helper()
	r, err := f.c.ChannelCreateForumTopic(&channel.TLChannelCreateForumTopic{
		SelfId: self, PermAuthKeyId: 1, ChannelId: id, Title: title, IconColor: 0x6FB9F0, TitleMissing: mtproto.BoolFalse, RandomId: randomId,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			return u.GetMessage_MESSAGE().GetId()
		}
	}
	f.t.Fatalf("no new message in %v", r)
	return 0
}

// inTopic writes a message of from to the topic, as msg does for a
// forum: its row names the topic, and from has read the topic up to it.
func (f *diffFixture) inTopic(id, from int64, topic int32) int32 {
	f.t.Helper()
	var last int32
	_, err := userbox.Run(context.Background(), f.box, from, func(b *userbox.Box) error {
		var row struct {
			Pts int32 `db:"pts"`
			Id  int32 `db:"last_message_id"`
		}
		if err := b.Tx().QueryRow(&row, "SELECT pts, last_message_id FROM channels WHERE id = ? FOR UPDATE", id); err != nil {
			return err
		}
		last = row.Id + 1
		column := topic
		if topic == 1 {
			column = 0
		}
		m := mtproto.MakeTLMessage(&mtproto.Message{Id: last, PeerId: mtproto.MakePeerChannel(id), FromId: mtproto.MakePeerUser(from), Message: "x", Date: 1}).To_Message()
		if topic != 1 {
			m.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ForumTopic: true, ReplyToMsgId: topic, ReplyToMsgId_INT32: topic, ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(topic)}).To_MessageReplyHeader()
		}
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET last_message_id = ?, top_message = ?, pts = pts + 1 WHERE id = ?", last, last, id); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channel_forum_topics SET top_message = ? WHERE channel_id = ? AND topic_id = ?", last, id, topic); err != nil {
			return err
		}
		_, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_data, message, topic_id, date2) VALUES (?, ?, ?, 0, ?, ?, 'x', ?, 1)",
			id, last, from, row.Pts+1, data, column)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return last
}

func (f *diffFixture) topics(self, id int64) *mtproto.Messages_ForumTopics {
	f.t.Helper()
	r, err := f.c.ChannelGetForumTopics(&channel.TLChannelGetForumTopics{SelfId: self, ChannelId: id, Limit: 20})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func topicOf(r *mtproto.Messages_ForumTopics, id int32) *mtproto.ForumTopic {
	for _, t := range r.GetTopics() {
		if t.GetId() == id {
			return t
		}
	}
	return nil
}

// TestForumToggle: only the creator of a supergroup turns it on; a supergroup
// that is a forum gets the General topic with its messages; a repeat is
// CHAT_NOT_MODIFIED; a discussion group cannot be one.
func TestForumToggle(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, creator, 2)
	in := &channel.TLChannelToggleForum{SelfId: bob, PermAuthKeyId: 1, ChannelId: id, Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse}
	_, err := f.c.ChannelToggleForum(in)
	wantErr(t, "a member", err, mtproto.ErrChatAdminRequired)
	if _, err = f.c.ChannelGetForumTopics(&channel.TLChannelGetForumTopics{SelfId: creator, ChannelId: id, Limit: 5}); err == nil {
		t.Fatal("no forum, topics given")
	} else {
		wantErr(t, "no forum", err, mtproto.ErrChannelForumMissing)
	}
	in.SelfId = creator
	if _, err = f.c.ChannelToggleForum(in); err != nil {
		t.Fatal(err)
	}
	_, err = f.c.ChannelToggleForum(in)
	wantErr(t, "a repeat", err, mtproto.ErrChatNotModified)
	r := f.topics(creator, id)
	if r.GetCount() != 1 || len(r.GetTopics()) != 1 || r.GetTopics()[0].GetId() != 1 || r.GetTopics()[0].GetTitle() != "General" {
		t.Fatalf("the General topic: %v", r.GetTopics())
	}
	var history []*mtproto.Message
	h, err := f.c.ChannelGetReplies(&channel.TLChannelGetReplies{SelfId: creator, ChannelId: id, MsgId: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	history = h.GetMessages()
	if len(history) != 3 {
		t.Errorf("the messages of before in General: %d, want Bob's join and 2", len(history))
	}
}

// TestForumTopics: a topic's root is its id and stands first in its
// history; a repeated create gives the first answer; the messages of a
// topic count in it alone; reading one topic leaves the others unread; a
// member edits its own topic, not another's; closing, pins, deletion.
func TestForumTopics(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.forum(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}

	a := f.createTopic(creator, id, "A", 777)
	if again := f.createTopic(creator, id, "A", 777); again != a {
		t.Errorf("a repeated create made topic %d, want %d", again, a)
	}
	b := f.createTopic(bob, id, "B", 778)
	inA := f.inTopic(id, creator, a)
	inB := f.inTopic(id, creator, b)

	bobs := f.topics(bob, id)
	if len(bobs.GetTopics()) != 3 || bobs.GetCount() != 3 {
		t.Fatalf("topics: %v", bobs.GetTopics())
	}
	if ta := topicOf(bobs, a); ta.GetUnreadCount() != 2 || ta.GetTopMessage() != inA || ta.GetMy() {
		t.Errorf("bob sees topic A: %v", ta)
	}
	if tb := topicOf(bobs, b); !tb.GetMy() || tb.GetUnreadCount() != 1 {
		t.Errorf("bob sees his topic B: %v", tb)
	}
	// One not in the public forum has nothing unread in it.
	stranger := topicOf(f.topics(f.user("Stranger"), id), a)
	if stranger.GetUnreadCount() != 0 || stranger.GetReadInboxMaxId() != inA || stranger.GetTopMessage() != inA {
		t.Errorf("a stranger sees topic A: %v", stranger)
	}
	h, err := f.c.ChannelGetReplies(&channel.TLChannelGetReplies{SelfId: bob, ChannelId: id, MsgId: a, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if h.GetCount() != 2 || h.GetMessages()[len(h.GetMessages())-1].GetId() != a {
		t.Errorf("topic A's history: %v", h.GetMessages())
	}
	if len(h.GetTopics()) != 1 || h.GetTopics()[0].GetId() != a {
		t.Errorf("topics of the history: %v", h.GetTopics())
	}

	if _, err = f.c.ChannelReadDiscussion(&channel.TLChannelReadDiscussion{SelfId: bob, PermAuthKeyId: 1, ChannelId: id, MsgId: a, ReadMaxId: inA}); err != nil {
		t.Fatal(err)
	}
	bobs = f.topics(bob, id)
	if ta, tb := topicOf(bobs, a), topicOf(bobs, b); ta.GetUnreadCount() != 0 || ta.GetReadInboxMaxId() != inA || tb.GetUnreadCount() != 1 {
		t.Errorf("after reading A: A %v, B %v", ta, tb)
	}
	dialogs, err := f.c.svcCtx.Dao.ChannelDialogs(context.Background(), bob, []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	// Topic B's message and the group's General: bob joined after them,
	// and read A; the dialog counts what is left of B's.
	if len(dialogs) != 1 || dialogs[0].GetUnreadCount() != 1 {
		t.Errorf("the dialog: %v", dialogs)
	}
	// Reading the whole forum below A's newest counts what A's reading
	// left: B's message alone, as the dialog does.
	if err = f.read(bob, id, a); err != nil {
		t.Fatal(err)
	}
	if us := f.readUpdates(bob); len(us) != 1 || us[0].GetStillUnreadCount() != 1 {
		t.Errorf("updateReadChannelInbox in a forum: %v", us)
	}

	edit := &channel.TLChannelEditForumTopic{SelfId: bob, PermAuthKeyId: 1, ChannelId: id, TopicId: a, Fields: channel.TopicFieldTitle, Title: "AA", Closed: mtproto.BoolFalse, Hidden: mtproto.BoolFalse}
	_, err = f.c.ChannelEditForumTopic(edit)
	wantErr(t, "another's topic", err, mtproto.ErrChatAdminRequired)
	edit.TopicId = b
	if _, err = f.c.ChannelEditForumTopic(edit); err != nil {
		t.Fatal(err)
	}
	_, err = f.c.ChannelEditForumTopic(edit)
	wantErr(t, "the same title", err, mtproto.ErrTopicNotModified)
	edit.Fields = channel.TopicFieldClosed | channel.TopicFieldTitle
	_, err = f.c.ChannelEditForumTopic(edit)
	wantErr(t, "closed with the title", err, mtproto.ErrTopicCloseSeparately)

	pin := func(self int64, topic int32, on bool) error {
		_, err := f.c.ChannelUpdatePinnedForumTopic(&channel.TLChannelUpdatePinnedForumTopic{SelfId: self, PermAuthKeyId: 1, ChannelId: id, TopicId: topic, Pinned: mtproto.ToBool(on)})
		return err
	}
	wantErr(t, "a member pins", pin(bob, a, true), mtproto.ErrChatAdminRequired)
	if err = pin(creator, a, true); err != nil {
		t.Fatal(err)
	}
	if r := f.topics(creator, id); r.GetTopics()[0].GetId() != a || !r.GetTopics()[0].GetPinned() {
		t.Errorf("the pinned topic first: %v", r.GetTopics())
	}
	if err = pin(creator, a, true); err == nil {
		t.Errorf("a pin repeated: %v", err)
	}
	// A search goes on after its last topic, a pinned one too: B's "AA"
	// and A, then General, not from the start again.
	page, err := f.c.ChannelGetForumTopics(&channel.TLChannelGetForumTopics{SelfId: creator, ChannelId: id, Q: "A", OffsetId: inA, OffsetTopic: a, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetTopics()) != 1 || page.GetTopics()[0].GetId() != 1 {
		t.Errorf("a search after a pinned topic: %v", page.GetTopics())
	}

	if _, err = f.c.ChannelReorderPinnedForumTopics(&channel.TLChannelReorderPinnedForumTopics{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Force: mtproto.BoolTrue, Order: []int32{b, a}}); err != nil {
		t.Fatal(err)
	}
	if r := f.topics(creator, id); r.GetTopics()[0].GetId() != b || r.GetTopics()[1].GetId() != a {
		t.Errorf("the order: %v", r.GetTopics())
	}

	del := func(self int64, topic int32) (*mtproto.Messages_AffectedHistory, error) {
		return f.c.ChannelDeleteTopicHistory(&channel.TLChannelDeleteTopicHistory{SelfId: self, PermAuthKeyId: 1, ChannelId: id, TopMsgId: topic})
	}
	_, err = del(bob, b)
	wantErr(t, "a member's topic with another's messages", err, mtproto.ErrMessageDeleteForbidden)
	_, err = del(creator, 1)
	wantErr(t, "General", err, mtproto.ErrTopicIdInvalid)
	if res, err := del(creator, b); err != nil || res.GetOffset() != 0 || res.GetPtsCount() != 3 {
		t.Fatalf("delete: %v %v", res, err)
	}
	by, err := f.c.ChannelGetForumTopicsByID(&channel.TLChannelGetForumTopicsByID{SelfId: creator, ChannelId: id, Topics: []int32{b, a}})
	if err != nil {
		t.Fatal(err)
	}
	if by.GetTopics()[0].GetPredicateName() != mtproto.Predicate_forumTopicDeleted || by.GetTopics()[1].GetId() != a {
		t.Errorf("by id: %v", by.GetTopics())
	}
	_, err = f.c.ChannelReadDiscussion(&channel.TLChannelReadDiscussion{SelfId: bob, PermAuthKeyId: 1, ChannelId: id, MsgId: b, ReadMaxId: inB})
	if err != nil {
		t.Errorf("reading a deleted topic: %v", err)
	}
}

// TestForumDeleteRootKept: channels.deleteMessages leaves the root of a
// topic; it goes only with the topic.
func TestForumDeleteRootKept(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	id := f.forum(creator)
	a := f.createTopic(creator, id, "A", 900)
	msg := f.inTopic(id, creator, a)
	r, err := f.c.ChannelDeleteMessages(&channel.TLChannelDeleteMessages{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Id: []int32{a, msg}})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPtsCount() != 1 {
		t.Errorf("deleted %d, want 1 (the root kept)", r.GetPtsCount())
	}
	if ta := topicOf(f.topics(creator, id), a); ta == nil || ta.GetTopMessage() != a {
		t.Errorf("topic after its message is deleted: %v", ta)
	}
}
