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
)

func (f *diffFixture) expireDue(channelId int64, ids ...int32) {
	f.t.Helper()
	for _, id := range ids {
		f.exec("INSERT INTO channel_message_expiry (channel_id, message_id, expire_at) VALUES (?, ?, 100)", channelId, id)
	}
}

func (f *diffFixture) expire(channelId int64) int {
	f.t.Helper()
	n, err := f.c.svcCtx.Dao.ExpireMessages(context.Background(), channelId, 1000)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *diffFixture) isDeleted(channelId int64, id int32) bool {
	f.t.Helper()
	var deleted bool
	if err := f.box.QueryRow("SELECT deleted FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, id).Scan(&deleted); err != nil {
		f.t.Fatal(err)
	}
	return deleted
}

// TestExpireMessagesTakesTheCopy: an expired post goes with its copy in the
// discussion group, and a second pass finds nothing (task 067).
func TestExpireMessagesTakesTheCopy(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	p := f.newDiscussionPost(creator, bob)
	f.expireDue(p.channelId, p.post)
	if n := f.expire(p.channelId); n != 1 {
		t.Fatalf("expired %d, want 1", n)
	}
	if !f.isDeleted(p.channelId, p.post) {
		t.Error("the post stayed")
	}
	if !f.isDeleted(p.groupId, p.copyId) {
		t.Error("its copy in the group stayed")
	}
	if f.isDeleted(p.groupId, p.comment) {
		t.Error("a comment went with the copy")
	}
	if n := f.copyRows(p); n != 0 {
		t.Errorf("%d links left", n)
	}
	if n := f.expire(p.channelId); n != 0 {
		t.Errorf("a second pass deleted %d", n)
	}
}

// TestExpireMessagesInTopic: the root of a topic stays, and the topic's top
// moves back to the message left.
func TestExpireMessagesInTopic(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	id := f.forum(creator)
	topic := f.createTopic(creator, id, "t", 7001)
	first := f.inTopic(id, creator, topic)
	second := f.inTopic(id, creator, topic)
	f.expireDue(id, topic, second)
	if n := f.expire(id); n != 1 {
		t.Fatalf("expired %d, want 1 (the root stays)", n)
	}
	if f.isDeleted(id, topic) {
		t.Error("the root of the topic went")
	}
	if !f.isDeleted(id, second) {
		t.Error("the message stayed")
	}
	var top int32
	if err := f.box.QueryRow("SELECT top_message FROM channel_forum_topics WHERE channel_id = ? AND topic_id = ?", id, topic).Scan(&top); err != nil {
		t.Fatal(err)
	}
	if top != first {
		t.Errorf("the topic's top %d, want %d", top, first)
	}
}

func (f *diffFixture) unreadReaction(channelId int64, msgId int32, from int64) {
	f.t.Helper()
	f.exec("INSERT INTO channel_message_reactions (channel_id, message_id, user_id, reaction, chosen_order, big, unread, `date`) VALUES (?, ?, ?, 'eX', 0, 0, 1, 1)",
		channelId, msgId, from)
}

// TestTopicCounters: unread reactions and poll votes, and settings, per
// topic and per dialog; reading a topic leaves the others (task 067).
func TestTopicCounters(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.forum(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	topic := f.createTopic(creator, id, "t", 7002)
	inTopic := f.inTopic(id, creator, topic)
	inGeneral := f.inTopic(id, creator, 1)
	f.unreadReaction(id, inTopic, bob)
	f.unreadReaction(id, inGeneral, bob)
	f.exec("INSERT INTO polls (id, creator_user_id, peer_type, peer_id, message_id, poll_data, correct_options, solution_data, has_unread, `date`) VALUES (?, ?, ?, ?, ?, '{}', '[]', '{}', 1, 1)",
		id*1000+1, creator, mtproto.PEER_CHANNEL, id, inTopic)
	f.exec("INSERT INTO user_topic_notify_settings (user_id, channel_id, top_msg_id, mute_until) VALUES (?, ?, ?, 2147483647)", creator, id, topic)

	r := f.topics(creator, id)
	tp := topicOf(r, topic)
	if tp.GetUnreadReactionsCount() != 1 || tp.GetUnreadPollVotesCount() != 1 {
		t.Errorf("the topic: reactions %d, votes %d", tp.GetUnreadReactionsCount(), tp.GetUnreadPollVotesCount())
	}
	if tp.GetNotifySettings().GetMuteUntil().GetValue() != 2147483647 {
		t.Errorf("the topic's settings: %v", tp.GetNotifySettings())
	}
	if topicOf(r, 1).GetUnreadReactionsCount() != 1 {
		t.Errorf("General: %d", topicOf(r, 1).GetUnreadReactionsCount())
	}
	if n := f.dialog(creator, id).GetUnreadReactionsCount(); n != 2 {
		t.Errorf("the dialog's unread reactions: %d", n)
	}
	if n := f.dialog(bob, id).GetUnreadReactionsCount(); n != 0 {
		t.Errorf("another member's: %d", n)
	}
	page, err := f.c.ChannelGetUnreadReactions(&channel.TLChannelGetUnreadReactions{SelfId: creator, ChannelId: id, TopMsgId: topic, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if ms := page.GetMessages(); len(ms) != 1 || ms[0].GetId() != inTopic {
		t.Errorf("unread reactions of the topic: %v", ms)
	}
	if _, err = f.c.ChannelReadReactions(&channel.TLChannelReadReactions{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, TopMsgId: topic}); err != nil {
		t.Fatal(err)
	}
	if n := f.dialog(creator, id).GetUnreadReactionsCount(); n != 1 {
		t.Errorf("the dialog after reading the topic: %d", n)
	}
	if n := topicOf(f.topics(creator, id), topic).GetUnreadReactionsCount(); n != 0 {
		t.Errorf("the topic after reading it: %d", n)
	}
}

// TestForumOffKeepsReads: reading only in topics survives a forum turned
// off as far as everything before is read; a root is then an ordinary
// message (task 067).
func TestForumOffKeepsReads(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.forum(creator)
	f.public(id)
	t1 := f.createTopic(creator, id, "one", 7003)
	t2 := f.createTopic(creator, id, "two", 7004)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	a := f.inTopic(id, creator, t1)
	b := f.inTopic(id, creator, t2)
	c := f.inTopic(id, creator, t1)
	f.exec("INSERT INTO channel_thread_reads (user_id, channel_id, top_id, read_max_id) VALUES (?, ?, ?, ?)", bob, id, t1, c)

	if _, err := f.c.ChannelToggleForum(&channel.TLChannelToggleForum{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Enabled: mtproto.BoolFalse, Tabs: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	if got := f.dialog(bob, id).GetReadInboxMaxId(); got != b-1 {
		t.Errorf("Bob's mark %d, want %d (read through %d, %d is unread)", got, b-1, a, b)
	}
	if n, err := f.delete(creator, id, t1); err != nil || n.GetPtsCount() != 1 {
		t.Errorf("deleting a root with the forum off: %v, %v", n, err)
	}
}

// TestImportersViaChatlist: a joiner by the link of a shared folder is
// marked via_chatlist; one by a plain link is not.
func TestImportersViaChatlist(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol := f.user("Creator"), f.user("Bob"), f.user("Carol")
	id := f.create(creator)
	f.exec("INSERT INTO chatlist_invite_peers (slug, channel_id, link) VALUES ('s', ?, 'folderlink')", id)
	f.exec("INSERT INTO chat_invite_participants (chat_id, link, user_id, requested, approved_by, date2) VALUES (?, 'folderlink', ?, 0, 0, 2)", id, bob)
	f.exec("INSERT INTO chat_invite_participants (chat_id, link, user_id, requested, approved_by, date2) VALUES (?, 'plainlink', ?, 0, 0, 1)", id, carol)
	rows, n, err := f.c.svcCtx.Dao.Importers(context.Background(), id, false, "", "", 0, 0, 10)
	if err != nil || n != 2 || len(rows) != 2 {
		t.Fatalf("importers: %v, %d, %v", rows, n, err)
	}
	if !rows[0].ViaChatlist || rows[1].ViaChatlist {
		t.Errorf("via_chatlist: %v", rows)
	}
}

// TestLeftChannelsSkipsCommenters: a row kept for the slow mode of a
// commenter never in the group is no channel it left (task 045).
func TestLeftChannelsSkipsCommenters(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	commented, left := f.create(creator), f.create(creator)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, last_send_date) VALUES (?, ?, 1, 5)", commented, bob)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, `date`) VALUES (?, ?, 1, 5)", left, bob)
	ids, n, err := f.c.svcCtx.Dao.LeftChannels(context.Background(), bob, 0)
	if err != nil || n != 1 || len(ids) != 1 || ids[0] != left {
		t.Errorf("left channels: %v, %d, %v, want only %d", ids, n, err, left)
	}
}
