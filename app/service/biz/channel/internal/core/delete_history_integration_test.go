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

	"github.com/zeromicro/go-zero/core/jsonx"
)

func (f *diffFixture) deleteHistory(self, channelId int64, forEveryone bool, maxId int32) (*mtproto.Updates, error) {
	return f.c.ChannelDeleteHistory(&channel.TLChannelDeleteHistory{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, ForEveryone: mtproto.ToBool(forEveryone), MaxId: maxId,
	})
}

func (f *diffFixture) deleteParticipant(self, channelId, userId int64) (*mtproto.Messages_AffectedHistory, error) {
	return f.c.ChannelDeleteParticipantHistory(&channel.TLChannelDeleteParticipantHistory{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, ParticipantId: userId,
	})
}

func (f *diffFixture) top(channelId int64) int32 {
	f.t.Helper()
	var top int32
	if err := f.box.QueryRow("SELECT top_message FROM channels WHERE id = ?", channelId).Scan(&top); err != nil {
		f.t.Fatal(err)
	}
	return top
}

// seqUpdates are the updates of the user's seq journal of a predicate.
func (f *diffFixture) seqUpdates(userId int64, predicate string) []*mtproto.Update {
	f.t.Helper()
	rows, err := f.box.Query("SELECT update_data FROM user_seq_updates WHERE user_id = ? AND update_type = ? ORDER BY seq", userId, predicate)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []*mtproto.Update
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			f.t.Fatal(err)
		}
		u := new(mtproto.Update)
		if err = jsonx.UnmarshalFromString(data, u); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, u)
	}
	if err = rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *diffFixture) dialog(self, channelId int64) *mtproto.Dialog {
	f.t.Helper()
	r, err := f.c.ChannelGetChannelDialogs(&channel.TLChannelGetChannelDialogs{SelfId: self, IdList: []int64{channelId}})
	if err != nil || len(r.GetDatas()) != 1 {
		f.t.Fatalf("the dialog: %v, %v", r, err)
	}
	return r.GetDatas()[0]
}

// TestDeleteHistoryForEveryone: only the creator may; a range up to max_id
// and then all of it, in one row of the journal each, the creation message
// stays, the mentions and top_message follow, a repeat changes nothing.
func TestDeleteHistoryForEveryone(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, bob, 2)
	f.send(id, creator, 2)
	top := f.top(id)
	f.exec("INSERT INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?)", id, bob, top-1)

	_, err := f.deleteHistory(bob, id, true, top)
	wantErr(t, "a member", err, mtproto.ErrChatAdminRequired)

	upto, err := f.deleteHistory(creator, id, true, top-2)
	if err != nil {
		t.Fatal(err)
	}
	if len(upto.GetUpdates()) != 1 || upto.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateDeleteChannelMessages {
		t.Fatalf("the answer: %v", upto)
	}
	deleted := upto.GetUpdates()[0]
	if int32(len(deleted.GetMessages())) != deleted.GetPtsCount() {
		t.Errorf("ids %v with pts_count %d", deleted.GetMessages(), deleted.GetPtsCount())
	}
	for _, m := range deleted.GetMessages() {
		if m <= 1 || m > top-2 {
			t.Errorf("message %d deleted by a range up to %d", m, top-2)
		}
	}
	if f.top(id) != top {
		t.Errorf("top_message %d moved", f.top(id))
	}

	all, err := f.deleteHistory(creator, id, true, 1<<31-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.GetUpdates()) != 1 || all.GetUpdates()[0].GetPtsCount() != 2 {
		t.Fatalf("all of it: %v", all)
	}
	pts := all.GetUpdates()[0].GetPts_INT32()
	if f.top(id) != 1 {
		t.Errorf("top_message %d, want the creation message", f.top(id))
	}
	var alive, mentions int
	if err = f.box.QueryRow("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND deleted = 0", id).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if alive != 1 {
		t.Errorf("%d messages left, want the creation message", alive)
	}
	if err = f.box.QueryRow("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ?", id).Scan(&mentions); err != nil {
		t.Fatal(err)
	}
	if mentions != 0 {
		t.Errorf("%d mentions left", mentions)
	}
	// One journal row each, contiguous: a member behind sees both.
	d := f.diff(bob, id, pts-all.GetUpdates()[0].GetPtsCount(), 10)
	if len(d.GetOtherUpdates()) != 1 || d.GetOtherUpdates()[0].GetPredicateName() != mtproto.Predicate_updateDeleteChannelMessages {
		t.Errorf("the difference: %v", d.GetOtherUpdates())
	}

	again, err := f.deleteHistory(creator, id, true, 0)
	if err != nil || len(again.GetUpdates()) != 0 {
		t.Errorf("a repeat: %v, %v", again, err)
	}
}

// TestDeleteHistoryPortions: a history longer than one portion goes in
// several journal rows, and the answer tells the client to fetch them.
func TestDeleteHistoryPortions(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	id := f.create(creator)
	f.send(id, creator, 1100)
	before := f.top(id)

	r, err := f.deleteHistory(creator, id, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannelTooLong {
		t.Fatalf("the answer: %v", r)
	}
	if got := r.GetUpdates()[0].GetPts_FLAGINT32().GetValue(); got == 0 {
		t.Errorf("no pts in updateChannelTooLong")
	}
	if f.top(id) != 1 || before < 1100 {
		t.Errorf("top_message %d (was %d)", f.top(id), before)
	}
	var rows int
	if err = f.box.QueryRow("SELECT COUNT(*) FROM channel_pts_updates WHERE channel_id = ? AND update_data LIKE ?", id, "%"+mtproto.Predicate_updateDeleteChannelMessages+"%").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows < 2 {
		t.Errorf("%d journal rows of deletions, want the portions", rows)
	}
}

// TestHideHistory: for oneself: others keep the messages; the difference, the
// dialog and the unread count go; the devices get the mark with seq; a repeat
// writes nothing; new messages show; one outside has nothing to hide.
func TestHideHistory(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, outsider := f.user("Creator"), f.user("Bob"), f.user("Out")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, creator, 3)
	top := f.top(id)
	f.exec("INSERT INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?)", id, bob, top)

	_, err := f.deleteHistory(outsider, id, false, top)
	wantErr(t, "one outside a public channel", err, mtproto.ErrChannelParicipantMissing)

	r, err := f.deleteHistory(bob, id, false, 1<<31-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannelAvailableMessages ||
		r.GetUpdates()[0].GetAvailableMinId() != top {
		t.Fatalf("the answer: %v", r)
	}
	if got := f.seqUpdates(bob, mtproto.Predicate_updateChannelAvailableMessages); len(got) != 1 || got[0].GetAvailableMinId() != top {
		t.Errorf("the seq row: %v", got)
	}
	if got := f.readUpdates(bob); len(got) != 1 || got[0].GetMaxId() != top || got[0].GetStillUnreadCount() != 0 {
		t.Errorf("the read mark for the devices: %v", got)
	}
	d := f.dialog(bob, id)
	if d.GetTopMessage() != 0 || d.GetUnreadCount() != 0 || d.GetUnreadMentionsCount() != 0 {
		t.Errorf("the dialog after hiding all: %v", d)
	}
	if got := f.dialog(creator, id); got.GetTopMessage() != top {
		t.Errorf("another's dialog changed: %v", got)
	}

	again, err := f.deleteHistory(bob, id, false, top)
	if err != nil || len(again.GetUpdates()) != 1 || again.GetUpdates()[0].GetAvailableMinId() != top {
		t.Errorf("a repeat: %v, %v", again, err)
	}
	if got := f.seqUpdates(bob, mtproto.Predicate_updateChannelAvailableMessages); len(got) != 1 {
		t.Errorf("a repeat journaled again: %d rows", len(got))
	}

	f.send(id, creator, 1)
	next := f.top(id)
	if got := f.dialog(bob, id); got.GetTopMessage() != next || got.GetUnreadCount() != 1 {
		t.Errorf("a new message after the clear: %v", got)
	}
	// The difference shows nothing of what was hidden, only the new one.
	diff := f.diff(bob, id, 0, 100)
	for _, m := range diff.GetNewMessages() {
		if m.GetId() <= top {
			t.Errorf("hidden message %d in the difference", m.GetId())
		}
	}
}

// TestDeleteParticipantHistory: only with the right; portions, the newest
// first, with an offset falling to 0; a broadcast channel is not for it.
func TestDeleteParticipantHistory(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, bob, 150)
	f.send(id, creator, 1)
	top := f.top(id)

	_, err := f.deleteParticipant(bob, id, creator)
	wantErr(t, "without the right", err, mtproto.ErrChatAdminRequired)

	first, err := f.deleteParticipant(creator, id, bob)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetPtsCount() != 100 || first.GetOffset() != 51 { // and the message of Bob's joining
		t.Fatalf("the first portion: %v", first)
	}
	second, err := f.deleteParticipant(creator, id, bob)
	if err != nil {
		t.Fatal(err)
	}
	if second.GetPtsCount() != 51 || second.GetOffset() != 0 || second.GetPts() != first.GetPts()+51 {
		t.Fatalf("the second portion: %v, after %v", second, first)
	}
	if f.top(id) != top {
		t.Errorf("top_message moved to %d (the creator's message %d stays)", f.top(id), top)
	}
	third, err := f.deleteParticipant(creator, id, bob)
	if err != nil || third.GetPtsCount() != 0 || third.GetOffset() != 0 || third.GetPts() != second.GetPts() {
		t.Fatalf("nothing left: %v, %v", third, err)
	}

	broadcast := f.create(creator)
	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", broadcast)
	_, err = f.deleteParticipant(creator, broadcast, bob)
	wantErr(t, "a broadcast channel", err, mtproto.ErrChannelInvalid)
}

// TestDeleteParticipantHistoryKeepsPosts: the copy of a channel's post in its
// discussion group is the channel's message, though its row names the admin
// who posted: deleting that user's messages leaves it, and the thread counts
// only the comments left.
func TestDeleteParticipantHistoryKeepsPosts(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	group := f.create(creator)
	f.public(group)
	if _, err := f.join(bob, group, ""); err != nil {
		t.Fatal(err)
	}
	f.send(group, bob, 2)
	f.send(group, creator, 1)
	top := f.top(group)
	root := top - 2
	f.exec("INSERT INTO channel_discussion_posts (channel_id, post_id, group_id, group_message_id) VALUES (?, 1, ?, ?)", group+1, group, root)
	f.exec("UPDATE channel_messages SET reply_top_id = ? WHERE channel_id = ? AND id > ?", root, group, root)

	r, err := f.deleteParticipant(creator, group, bob)
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPtsCount() != 2 || r.GetOffset() != 0 { // his comment and the message of his joining
		t.Errorf("the answer: %v", r)
	}
	var deleted bool
	if err = f.box.QueryRow("SELECT deleted FROM channel_messages WHERE channel_id = ? AND id = ?", group, root).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Errorf("the copy of the post %d was deleted with the messages of its author", root)
	}
	thread, err := f.c.svcCtx.Dao.ThreadOf(context.Background(), group, root)
	if err != nil {
		t.Fatal(err)
	}
	if thread.Count != 1 || thread.MaxId != top {
		t.Errorf("the thread after the deletion: %d comments up to %d, want 1 up to %d", thread.Count, thread.MaxId, top)
	}
}
