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

//go:build integration

package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// readsWorld writes the rows msg keeps for reads: copies of messages,
// dialogs with their read boundaries, the dates of reads.
type readsWorld struct {
	t *testing.T
}

func (w readsWorld) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := itRaw.Exec(query, args...); err != nil {
		w.t.Fatal(err)
	}
}

// copyOf writes userId's copy boxId of the group message dialogMessageId.
func (w readsWorld) copyOf(userId int64, chat int64, boxId int32, dialogMessageId, sender int64) {
	w.exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_filter_type, message_data, deleted)"+
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, '{}', 0)",
		userId, boxId, -chat, -chat, dialogMessageId, sender, mtproto.PEER_CHAT, chat)
}

func (w readsWorld) dialog(userId int64, peerType int32, peerId int64, readInbox, readOutbox int32) {
	w.exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, read_inbox_max_id, read_outbox_max_id, draft_message_data, date2)"+
		" VALUES (?, ?, ?, ?, ?, ?, 'null', 0)",
		userId, peerType, peerId, mtproto.MakePeerDialogId(peerType, peerId), readInbox, readOutbox)
}

func (w readsWorld) read(userId, peerDialogId, reader int64, minId, maxId int32, date int64) {
	w.exec("INSERT INTO message_read_outbox (user_id, peer_dialog_id, read_user_id, read_outbox_min_id, read_outbox_max_id, read_outbox_max_date)"+
		" VALUES (?, ?, ?, ?, ?, ?)", userId, peerDialogId, reader, minId, maxId, date)
}

// TestReaders: who read a message is told by the readers' read
// boundaries, when by the read whose range holds it; a read from before
// the dates were kept has the date 0, and the latest come first.
func TestReaders(t *testing.T) {
	const (
		user, chat, peer = int64(3001), int64(3002), int64(3003)
		bob, carol, dave = int64(3011), int64(3012), int64(3013)
		erin             = int64(3014)
	)
	w := readsWorld{t}
	chatDialog := mtproto.MakePeerDialogId(mtproto.PEER_CHAT, chat)
	for _, u := range []int64{user, bob, carol, dave, erin} {
		w.exec("INSERT INTO chat_participants (chat_id, user_id, state) VALUES (?, ?, 0)", chat, u)
	}
	// user sent 10, 11 and 12 to the group: dialog_message_id 9010-9012.
	for i, id := range []int32{10, 11, 12} {
		w.copyOf(user, chat, id, int64(9010+i), user)
	}
	// Bob read 10 and 11 at 1100 and 12 at 1200; Carol read all three at
	// 1300; Dave read 10 and 11 before the dates were kept and 12 at 1400;
	// Erin read nothing.
	for _, r := range []struct {
		reader    int64
		firstCopy int32
		readInbox int32
	}{{bob, 100, 102}, {carol, 200, 202}, {dave, 300, 302}, {erin, 400, 0}} {
		for i := range int32(3) {
			w.copyOf(r.reader, chat, r.firstCopy+i, int64(9010)+int64(i), user)
		}
		w.dialog(r.reader, mtproto.PEER_CHAT, chat, r.readInbox, 0)
	}
	w.read(user, chatDialog, bob, 10, 11, 1100)
	w.read(user, chatDialog, bob, 12, 12, 1200)
	w.read(user, chatDialog, carol, 10, 12, 1300)
	w.read(user, chatDialog, dave, 12, 12, 1400)
	// A private chat: peer read 20 at 1500 in a row written before the
	// ranges, and 25 is not read.
	w.dialog(user, mtproto.PEER_USER, peer, 0, 20)
	w.read(user, mtproto.MakePeerDialogId(mtproto.PEER_USER, peer), peer, 0, 20, 1500)

	c := New(context.Background(), itSvc)
	readers := func(peerType int32, peerId int64, msgId int32) string {
		t.Helper()
		r, err := c.MessageGetOutboxReadDate(&message.TLMessageGetOutboxReadDate{UserId: user, PeerType: peerType, PeerId: peerId, MsgId: msgId})
		if err != nil {
			t.Fatal(err)
		}
		s := ""
		for _, d := range r.GetDatas() {
			s += fmt.Sprintf("%d@%d ", d.GetUserId(), d.GetDate())
		}
		return s
	}
	for _, tc := range []struct {
		peerType int32
		peerId   int64
		msgId    int32
		want     string
	}{
		{mtproto.PEER_CHAT, chat, 10, "3012@1300 3011@1100 3013@0 "},
		{mtproto.PEER_CHAT, chat, 12, "3013@1400 3012@1300 3011@1200 "},
		{mtproto.PEER_USER, peer, 18, "3003@1500 "},
		{mtproto.PEER_USER, peer, 25, ""},
	} {
		if got := readers(tc.peerType, tc.peerId, tc.msgId); got != tc.want {
			t.Errorf("readers of %d in %d/%d: %q, want %q", tc.msgId, tc.peerType, tc.peerId, got, tc.want)
		}
	}
}
