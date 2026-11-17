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

package business

import (
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The bot writes in a chat whose last incoming message is at most 24 hours
// old (businessBotRights.reply); a chat with none is closed to it.
func TestWithinReplyWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		last int64
		want bool
	}{
		{0, false},
		{now.Unix(), true},
		{now.Add(-23 * time.Hour).Unix(), true},
		{now.Add(-24 * time.Hour).Unix(), true},
		{now.Add(-24*time.Hour - time.Second).Unix(), false},
		{now.Add(-48 * time.Hour).Unix(), false},
	} {
		if got := withinReplyWindow(tc.last, now); got != tc.want {
			t.Errorf("last %d: %v, want %v", now.Unix()-tc.last, got, tc.want)
		}
	}
}

// Every method of the connection has its rule, and the methods of the
// right to reply are bound to the 24 hours.
func TestMethodRules(t *testing.T) {
	for m := MethodSendMessage; m <= MethodUploadMedia; m++ {
		r, ok := methodRules[m]
		if !ok {
			t.Fatalf("method %d has no rule", m)
		}
		if r.right == RightReply && (!r.chat || !r.recent) {
			t.Errorf("method %d: %+v", m, r)
		}
	}
	if r := methodRules[MethodReadHistory]; r.right != RightReadMessages || r.recent || !r.chat {
		t.Errorf("readHistory: %+v", r)
	}
	for _, m := range []Method{MethodDeleteMessages, MethodUpdateProfile} {
		if r := methodRules[m]; r.chat || r.right != 0 {
			t.Errorf("method %d: %+v", m, r)
		}
	}
}

func testAct() *Act {
	return &Act{
		Connection: &Connection{UserID: 1, BotID: 50, ConnectionID: "c1", Rights: RightReply},
		PeerID:     2,
	}
}

func viaBot(id int64, out bool, peer int64) *mtproto.Message {
	m := &mtproto.Message{Out: out, PeerId: mtproto.MakePeerUser(peer)}
	if id != 0 {
		m.ViaBusinessBotId = wrapperspb.Int64(id)
	}
	return mtproto.MakeTLMessage(m).To_Message()
}

// The bot edits only what it sent itself, in the chat of the request.
func TestCheckEdit(t *testing.T) {
	a := testAct()
	for _, tc := range []struct {
		name string
		m    *mtproto.Message
		ok   bool
	}{
		{"sent by the bot", viaBot(50, true, 2), true},
		{"sent by the account", viaBot(0, true, 2), false},
		{"sent by another bot", viaBot(51, true, 2), false},
		{"incoming", viaBot(0, false, 1), false},
		{"of another chat", viaBot(50, true, 3), false},
	} {
		err := a.CheckEdit(tc.m)
		if tc.ok != (err == nil) {
			t.Errorf("%s: %v", tc.name, err)
		}
		if err != nil && err != mtproto.ErrBotAccessForbidden {
			t.Errorf("%s: %v, want BOT_ACCESS_FORBIDDEN", tc.name, err)
		}
	}
}

// The answer to the bot carries the account's messages as business updates
// of the connection and nothing of the account's pts.
func TestBotUpdates(t *testing.T) {
	a := testAct()
	sent := viaBot(50, true, 2)
	sent.Id = 77
	sent.FromId = mtproto.MakePeerUser(1)
	in := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{
			mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: 77, RandomId: 9}).To_Update(),
			mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: sent, Pts_INT32: 500, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateEditMessage(&mtproto.Update{Message_MESSAGE: sent, Pts_INT32: 501, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdatePinnedMessages(&mtproto.Update{Messages: []int32{77}, Pts_INT32: 502, PtsCount: 1}).To_Update(),
		},
		Date: 1234,
	}).To_Updates()
	out, users := a.BotUpdates(in)
	if out.GetPredicateName() != mtproto.Predicate_updates || out.GetDate() != 1234 || out.GetSeq() != 0 {
		t.Fatalf("answer %s date %d seq %d", out.GetPredicateName(), out.GetDate(), out.GetSeq())
	}
	if len(out.GetUpdates()) != 2 {
		t.Fatalf("%d updates, want 2", len(out.GetUpdates()))
	}
	for i, want := range []string{mtproto.Predicate_updateBotNewBusinessMessage, mtproto.Predicate_updateBotEditBusinessMessage} {
		u := out.GetUpdates()[i]
		if u.GetPredicateName() != want || u.GetConnectionId() != "c1" || u.GetMessage_MESSAGE().GetId() != 77 || u.GetQts() != 0 || u.GetPts_INT32() != 0 {
			t.Errorf("update %d: %s %q id %d qts %d pts %d", i, u.GetPredicateName(), u.GetConnectionId(), u.GetMessage_MESSAGE().GetId(), u.GetQts(), u.GetPts_INT32())
		}
	}
	if !slices.Contains(users, 1) || !slices.Contains(users, 2) || !slices.Contains(users, 50) {
		t.Errorf("users %v", users)
	}

	// An answer with nothing for the bot is empty, not the account's.
	empty, _ := a.BotUpdates(mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateReadHistoryOutbox(&mtproto.Update{Pts_INT32: 7}).To_Update()},
	}).To_Updates())
	if len(empty.GetUpdates()) != 0 {
		t.Errorf("%d updates of the account's state", len(empty.GetUpdates()))
	}
}
