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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// discussionPost is a broadcast channel of creator with a discussion group
// bob is in, a post with its copy, and bob's comment in the copy's thread.
type discussionPost struct {
	channelId, groupId    int64
	post, copyId, comment int32
}

func (f *diffFixture) stored(channelId int64, id int32) *mtproto.Message {
	f.t.Helper()
	var data string
	if err := f.box.QueryRow("SELECT message_data FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, id).Scan(&data); err != nil {
		f.t.Fatal(err)
	}
	var m *mtproto.Message
	if err := jsonx.UnmarshalFromString(data, &m); err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *diffFixture) newDiscussionPost(creator, bob int64) *discussionPost {
	f.t.Helper()
	channelId := f.create(creator)
	return f.discussionPostIn(channelId, f.create(creator), creator, bob)
}

func (f *diffFixture) discussionPostIn(channelId, groupId, creator, bob int64) *discussionPost {
	f.t.Helper()
	p := &discussionPost{channelId: channelId, groupId: groupId}
	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0, linked_chat_id = ? WHERE id = ?", p.groupId, p.channelId)
	f.exec("UPDATE channels SET linked_chat_id = ? WHERE id = ?", p.channelId, p.groupId)
	f.public(p.groupId)
	if _, err := f.join(bob, p.groupId, ""); err != nil {
		f.t.Fatal(err)
	}
	f.send(p.channelId, creator, 1)
	p.post = f.top(p.channelId)
	m := f.stored(p.channelId, p.post)
	m.Replies = mtproto.MakeTLMessageReplies(&mtproto.MessageReplies{Comments: true, ChannelId: mtproto.MakeFlagsInt64(p.groupId), RecentRepliers: []*mtproto.Peer{}}).To_MessageReplies()
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec("UPDATE channel_messages SET message_data = ? WHERE channel_id = ? AND id = ?", data, p.channelId, p.post)
	f.send(p.groupId, creator, 1)
	p.copyId = f.top(p.groupId)
	f.exec("INSERT INTO channel_discussion_posts (channel_id, post_id, group_id, group_message_id) VALUES (?, ?, ?, ?)", p.channelId, p.post, p.groupId, p.copyId)
	f.send(p.groupId, bob, 1)
	p.comment = f.top(p.groupId)
	f.exec("UPDATE channel_messages SET reply_top_id = ? WHERE channel_id = ? AND id = ?", p.copyId, p.groupId, p.comment)
	return p
}

func (f *diffFixture) discussionMessage(self, channelId int64, id int32) (*mtproto.Messages_DiscussionMessage, error) {
	return f.c.ChannelGetDiscussionMessage(&channel.TLChannelGetDiscussionMessage{SelfId: self, ChannelId: channelId, MsgId: id})
}

func (f *diffFixture) copyRows(p *discussionPost) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_discussion_posts WHERE channel_id = ? AND post_id = ?", p.channelId, p.post).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestDeletePostWithComments: a deleted post takes its copy with it, by a
// deletion with a pts of the group's, whoever deleted it; the comments stay
// and their thread still opens, with no first message; the post has no
// comments to open.
func TestDeletePostWithComments(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	p := f.newDiscussionPost(creator, bob)
	f.send(p.groupId, bob, 1)
	groupTop := f.top(p.groupId)

	before := f.journal(p.groupId)
	r, err := f.delete(creator, p.channelId, p.post)
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPtsCount() != 1 {
		t.Errorf("pts_count %d: the channel's own deletion", r.GetPtsCount())
	}
	after := f.journal(p.groupId)
	if len(after) != len(before)+1 {
		t.Fatalf("the group's journal: %v after %v", after, before)
	}
	d := f.diff(creator, p.groupId, after[len(after)-1]-1, 10)
	if len(d.GetOtherUpdates()) != 1 || !slices.Equal(d.GetOtherUpdates()[0].GetMessages(), []int32{p.copyId}) {
		t.Errorf("the group's deletion: %v", d.GetOtherUpdates())
	}
	if n := f.copyRows(p); n != 0 {
		t.Errorf("%d links left", n)
	}
	if got := f.top(p.groupId); got != groupTop {
		t.Errorf("the group's top message %d, want %d", got, groupTop)
	}

	if _, err = f.discussionMessage(creator, p.channelId, p.post); err != mtproto.ErrMsgIdInvalid {
		t.Errorf("the comments of a deleted post: %v", err)
	}
	dm, err := f.discussionMessage(bob, p.groupId, p.comment)
	if err != nil {
		t.Fatalf("the thread of a deleted copy: %v", err)
	}
	if len(dm.GetMessages()) != 0 || dm.GetMaxId().GetValue() != p.comment {
		t.Errorf("the thread: %v, max %d", dm.GetMessages(), dm.GetMaxId().GetValue())
	}
	replies, err := f.c.ChannelGetReplies(&channel.TLChannelGetReplies{SelfId: bob, ChannelId: p.groupId, MsgId: p.copyId, Limit: 10})
	if err != nil {
		t.Fatalf("messages.getReplies by the deleted copy: %v", err)
	}
	if m := replies.GetMessages(); len(m) != 1 || m[0].GetId() != p.comment {
		t.Errorf("the comments left: %v", m)
	}
}

// TestDeletePostGroupFirst: the two are locked in ascending order when the
// group has the lower id too.
func TestDeletePostGroupFirst(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	groupId := f.create(creator)
	p := f.discussionPostIn(f.create(creator), groupId, creator, bob)
	if p.groupId >= p.channelId {
		t.Fatalf("group %d, channel %d", p.groupId, p.channelId)
	}
	before := f.journal(p.groupId)
	if _, err := f.delete(creator, p.channelId, p.post); err != nil {
		t.Fatal(err)
	}
	if after := f.journal(p.groupId); len(after) != len(before)+1 {
		t.Errorf("the group's journal: %v after %v", after, before)
	}
}

// TestDeletedMessageWithoutThread: the thread of a deleted message that has
// no replies does not open.
func TestDeletedMessageWithoutThread(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	groupId := f.create(creator)
	f.send(groupId, creator, 1)
	id := f.top(groupId)
	if _, err := f.delete(creator, groupId, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.discussionMessage(creator, groupId, id); err != mtproto.ErrMsgIdInvalid {
		t.Errorf("the thread of a deleted message: %v", err)
	}
}

// TestDeleteCopyOfOldPost: a post stored before posts carried their
// comments had them shown by its link, and loses them the same way.
func TestDeleteCopyOfOldPost(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	p := f.newDiscussionPost(creator, bob)
	m := f.stored(p.channelId, p.post)
	m.Replies = nil
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE channel_messages SET message_data = ? WHERE channel_id = ? AND id = ?", data, p.channelId, p.post)

	before := f.journal(p.channelId)
	if _, err = f.delete(creator, p.groupId, p.copyId); err != nil {
		t.Fatal(err)
	}
	if after := f.journal(p.channelId); len(after) != len(before)+1 {
		t.Errorf("the channel's journal: %v after %v", after, before)
	}
}

// TestDeleteCopyTurnsCommentsOff: the group's admin removes the copy; the
// post loses its comments, by a change with a pts and no edit date.
func TestDeleteCopyTurnsCommentsOff(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	p := f.newDiscussionPost(creator, bob)

	before := f.journal(p.channelId)
	if _, err := f.delete(creator, p.groupId, p.copyId); err != nil {
		t.Fatal(err)
	}
	after := f.journal(p.channelId)
	if len(after) != len(before)+1 {
		t.Fatalf("the channel's journal: %v after %v", after, before)
	}
	d := f.diff(creator, p.channelId, after[len(after)-1]-1, 10)
	if len(d.GetOtherUpdates()) != 1 || d.GetOtherUpdates()[0].GetPredicateName() != mtproto.Predicate_updateEditChannelMessage {
		t.Fatalf("the channel's change: %v", d.GetOtherUpdates())
	}
	if m := d.GetOtherUpdates()[0].GetMessage_MESSAGE(); m.GetId() != p.post || m.GetReplies() != nil || m.GetEditDate() != nil {
		t.Errorf("the post: %v", m)
	}
	if m := f.stored(p.channelId, p.post); m.GetReplies() != nil {
		t.Errorf("the stored post keeps its comments: %v", m.GetReplies())
	}
	if n := f.copyRows(p); n != 0 {
		t.Errorf("%d links left", n)
	}
	if _, err := f.discussionMessage(creator, p.channelId, p.post); err != mtproto.ErrMsgIdInvalid {
		t.Errorf("the comments of the post: %v", err)
	}
}

// TestReadDiscussionUpdates: a reader's read tells its devices, with the
// post of the thread, and the authors of the replies read; a read that
// moves nothing tells nobody; the read stops at the thread's last message;
// the author sees how far its replies were read.
func TestReadDiscussionUpdates(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	p := f.newDiscussionPost(creator, bob)
	f.public(p.channelId)

	discussionReads := func(userId int64) int {
		t.Helper()
		var n int
		if err := f.box.QueryRow("SELECT COUNT(*) FROM user_seq_updates WHERE user_id = ? AND update_type = ?", userId, mtproto.Predicate_updateReadChannelDiscussionInbox).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	read := func(self int64, msgId, readMax int32) {
		t.Helper()
		if _, err := f.c.ChannelReadDiscussion(&channel.TLChannelReadDiscussion{SelfId: self, PermAuthKeyId: 1, ChannelId: p.groupId, MsgId: msgId, ReadMaxId: readMax}); err != nil {
			t.Fatal(err)
		}
	}
	pushes := f.outboxPushes(bob)
	postReadMax := func(self int64) int32 {
		t.Helper()
		m, err := f.c.svcCtx.Dao.Messages(context.Background(), self, p.channelId, []int32{p.post})
		if err != nil || len(m) != 1 {
			t.Fatalf("the post: %v, %v", m, err)
		}
		return m[0].GetReplies().GetReadMaxId().GetValue()
	}

	read(creator, p.copyId, p.comment+100)
	if got := postReadMax(creator); got != p.comment {
		t.Errorf("the post's read_max_id for the reader: %d, want %d", got, p.comment)
	}
	if got := postReadMax(bob); got != 0 {
		t.Errorf("the post's read_max_id for another: %d", got)
	}
	if n := discussionReads(creator); n != 1 {
		t.Fatalf("%d inbox updates", n)
	}
	var data string
	if err := f.box.QueryRow("SELECT update_data FROM user_seq_updates WHERE user_id = ? AND update_type = ?", creator, mtproto.Predicate_updateReadChannelDiscussionInbox).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var u mtproto.Update
	if err := jsonx.UnmarshalFromString(data, &u); err != nil {
		t.Fatal(err)
	}
	if u.GetChannelId() != p.groupId || u.GetTopMsgId_INT32() != p.copyId || u.GetReadMaxId() != p.comment ||
		u.GetBroadcastId().GetValue() != p.channelId || u.GetBroadcastPost().GetValue() != p.post {
		t.Errorf("the inbox update: %v", &u)
	}
	if n := f.outboxPushes(bob); n != pushes+1 {
		t.Errorf("%d outbox pushes to the author, want %d", n, pushes+1)
	}

	read(creator, p.copyId, p.comment)
	if n := discussionReads(creator); n != 1 {
		t.Errorf("a repeated read wrote: %d updates", n)
	}
	if n := f.outboxPushes(bob); n != pushes+1 {
		t.Errorf("a repeated read pushed: %d, want %d", n, pushes+1)
	}

	dm, err := f.discussionMessage(bob, p.groupId, p.comment)
	if err != nil {
		t.Fatal(err)
	}
	if dm.GetReadOutboxMaxId().GetValue() != p.comment {
		t.Errorf("read_outbox_max_id %d, want %d", dm.GetReadOutboxMaxId().GetValue(), p.comment)
	}
}
