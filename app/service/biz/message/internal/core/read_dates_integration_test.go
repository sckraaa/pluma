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

// TestOutboxReadDates: the readers of a message with the dates they read
// it, the latest first: each reader's first read at or above the message,
// in the message's dialog only.
func TestOutboxReadDates(t *testing.T) {
	const user, chat, peer = int64(3001), int64(3002), int64(3003)
	chatDialog := mtproto.MakePeerDialogId(mtproto.PEER_CHAT, chat)
	privateDialog := mtproto.MakePeerDialogId(mtproto.PEER_USER, peer)
	for _, r := range []struct {
		dialog, reader int64
		maxId          int32
		date           int64
	}{
		{chatDialog, 8, 9, 1000}, // before the message
		{chatDialog, 8, 12, 1100},
		{chatDialog, 8, 20, 1200},
		{chatDialog, 9, 15, 1300},
		{chatDialog, 10, 9, 1400},       // not as far as the message
		{privateDialog, peer, 30, 1500}, // the private chat's, of a later message
	} {
		if _, err := itRaw.Exec("INSERT INTO message_read_outbox"+
			" (user_id, peer_dialog_id, read_user_id, read_outbox_max_id, read_outbox_max_date) VALUES (?, ?, ?, ?, ?)",
			user, r.dialog, r.reader, r.maxId, r.date); err != nil {
			t.Fatal(err)
		}
	}

	c := New(context.Background(), itSvc)
	dates := func(peerType int32, peerId int64, msgId int32) string {
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
		{mtproto.PEER_CHAT, chat, 10, "9@1300 8@1100 "},
		{mtproto.PEER_CHAT, chat, 21, ""},
		{mtproto.PEER_USER, peer, 25, fmt.Sprintf("%d@1500 ", peer)},
		{mtproto.PEER_USER, peer, 31, ""},
	} {
		if got := dates(tc.peerType, tc.peerId, tc.msgId); got != tc.want {
			t.Errorf("dates of %d in %d/%d: %q, want %q", tc.msgId, tc.peerType, tc.peerId, got, tc.want)
		}
	}
}
