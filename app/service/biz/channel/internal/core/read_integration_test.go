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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"

	"github.com/zeromicro/go-zero/core/jsonx"
)

func (f *diffFixture) read(self, channelId int64, maxId int32) error {
	_, err := f.c.ChannelReadHistory(&channel.TLChannelReadHistory{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, MaxId: maxId})
	return err
}

// readUpdates are the updateReadChannelInbox of the user's seq journal.
func (f *diffFixture) readUpdates(userId int64) []*mtproto.Update {
	f.t.Helper()
	rows, err := f.box.Query("SELECT update_data FROM user_seq_updates WHERE user_id = ? AND update_type = ? ORDER BY seq", userId, mtproto.Predicate_updateReadChannelInbox)
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

func (f *diffFixture) outboxPushes(recipient int64) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND method = ?", recipient, "sync.TL_sync_pushUpdates").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestRead: a member's read moves its dialog forward only, reads its
// mentions, journals updateReadChannelInbox with the channel's pts and the
// count still unread, and tells the authors of what it read; one not in
// the channel has nothing to read.
func TestRead(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, outsider := f.user("Creator"), f.user("Bob"), f.user("Out")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	pts := f.send(id, creator, 3)
	var last int32
	if err := f.box.QueryRow("SELECT top_message FROM channels WHERE id = ?", id).Scan(&last); err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?), (?, ?, ?)", id, bob, last-1, id, bob, last)
	pushes := f.outboxPushes(creator)

	if err := f.read(bob, id, last-1); err != nil {
		t.Fatal(err)
	}
	var read, unreadMentions int32
	if err := f.box.QueryRow("SELECT read_inbox_max_id FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?", bob, mtproto.PEER_CHANNEL, id).Scan(&read); err != nil {
		t.Fatal(err)
	}
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND `read` = 0", id, bob).Scan(&unreadMentions); err != nil {
		t.Fatal(err)
	}
	if read != last-1 || unreadMentions != 1 {
		t.Errorf("read %d, unread mentions %d; want %d and 1", read, unreadMentions, last-1)
	}
	us := f.readUpdates(bob)
	if len(us) != 1 || us[0].GetMaxId() != last-1 || us[0].GetStillUnreadCount() != 1 || us[0].GetPts_INT32() != pts || us[0].GetChannelId() != id {
		t.Fatalf("updateReadChannelInbox %v, want max %d, 1 unread, pts %d", us, last-1, pts)
	}
	if got := f.outboxPushes(creator); got != pushes+1 {
		t.Errorf("pushes to the author %d, want %d", got, pushes+1)
	}

	for _, maxId := range []int32{1, last - 1} {
		if err := f.read(bob, id, maxId); err != nil {
			t.Fatal(err)
		}
	}
	if us = f.readUpdates(bob); len(us) != 1 {
		t.Errorf("a read behind journaled %d updates, want none more", len(us)-1)
	}
	if err := f.read(bob, id, 0); err != nil {
		t.Fatal(err)
	}
	if us = f.readUpdates(bob); len(us) != 2 || us[1].GetMaxId() != last || us[1].GetStillUnreadCount() != 0 {
		t.Errorf("reading all: %v", us)
	}
	wantErr(t, "one not in the channel", f.read(outsider, id, 0), mtproto.ErrChannelPrivate)
}

// TestReadContents: a member who has seen messages reads its mentions
// among them, not the others, and its devices get
// updateChannelReadMessagesContents with seq of the messages that exist;
// one not in the channel has nothing to read.
func TestReadContents(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, outsider := f.user("Creator"), f.user("Bob"), f.user("Out")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, creator, 3)
	var last int32
	if err := f.box.QueryRow("SELECT top_message FROM channels WHERE id = ?", id).Scan(&last); err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?), (?, ?, ?)", id, bob, last-1, id, bob, last)
	read := func(self int64, ids ...int32) error {
		_, err := f.c.ChannelReadMessageContents(&channel.TLChannelReadMessageContents{SelfId: self, PermAuthKeyId: 1, ChannelId: id, Id: ids})
		return err
	}
	if err := read(bob, last, last+100); err != nil {
		t.Fatal(err)
	}
	var unread int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND `read` = 0", id, bob).Scan(&unread); err != nil || unread != 1 {
		t.Errorf("unread mentions %d, %v", unread, err)
	}
	var data string
	if err := f.box.QueryRow("SELECT update_data FROM user_seq_updates WHERE user_id = ? AND update_type = ? ORDER BY seq DESC LIMIT 1",
		bob, mtproto.Predicate_updateChannelReadMessagesContents).Scan(&data); err != nil {
		t.Fatal(err)
	}
	u := new(mtproto.Update)
	if err := jsonx.UnmarshalFromString(data, u); err != nil || u.GetChannelId() != id || len(u.GetMessages()) != 1 || u.GetMessages()[0] != last {
		t.Errorf("the update %v, %v", u, err)
	}
	wantErr(t, "one not in the channel", read(outsider, last), mtproto.ErrChannelPrivate)
}
