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

package dao_test

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

const testTTL = 86400

func (w *world) setTTL(from int64, peerType int32, peerId int64, period int32) (*mtproto.Updates, error) {
	return w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: from, AuthKeyId: from * 10, PeerType: peerType, PeerId: peerId,
		Message: []*msg.OutboxMessage{outbox(mtproto.MakeTLMessageService(&mtproto.Message{
			Out:    true,
			FromId: mtproto.MakePeerUser(from),
			PeerId: mtproto.MakePeer(peerType, peerId),
			Date:   int32(time.Now().Unix()),
			Action: historyttl.Action(period, 0),
		}).To_Message())},
	})
}

func (w *world) dialogTTL(userId int64, peerType int32, peerId int64) int32 {
	w.t.Helper()
	var ttl int32
	if err := w.check.QueryRow("SELECT ttl_period FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?", userId, peerType, peerId).Scan(&ttl); err != nil {
		w.t.Fatal(err)
	}
	return ttl
}

func (w *world) countRows(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.check.QueryRow(query, args...).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// A period set in a private chat reaches both sides; messages sent after it
// carry it, and each box deletes its own copy when due: with its pts, its
// dialog's top message, and only once however many passes run.
func TestAutoDeletePrivate(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()

	w.send(a, mtproto.PEER_USER, b, "before")
	w.pub.drain()
	if _, err := w.setTTL(a, mtproto.PEER_USER, b, testTTL); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	if got := w.dialogTTL(a, mtproto.PEER_USER, b); got != testTTL {
		t.Errorf("sender's dialog period %d", got)
	}
	if got := w.dialogTTL(b, mtproto.PEER_USER, a); got != testTTL {
		t.Errorf("recipient's dialog period %d", got)
	}

	r := w.send(a, mtproto.PEER_USER, b, "goes")
	if got := r.GetUpdates()[len(r.GetUpdates())-1].GetMessage_MESSAGE().GetTtlPeriod().GetValue(); got != testTTL {
		t.Fatalf("ttl_period of the new message: %d", got)
	}
	w.pub.drain()
	w.send(b, mtproto.PEER_USER, a, "goes too")
	w.pub.drain()

	if n := w.countRows("SELECT COUNT(*) FROM message_expiry WHERE user_id = ?", a); n != 2 {
		t.Fatalf("expiry rows of the sender: %d, want 2", n)
	}
	beforeA, beforeB := w.state(a), w.state(b)

	// Nothing of theirs is due yet.
	if _, err := w.d.ExpireDue(ctx, time.Now().Unix()+100); err != nil {
		t.Fatalf("early pass: %v", err)
	}
	if st := w.state(a); st.Pts != beforeA.Pts {
		t.Fatalf("the early pass moved pts %d -> %d", beforeA.Pts, st.Pts)
	}
	n, err := w.d.ExpireDue(ctx, time.Now().Unix()+testTTL+100)
	if err != nil || n < 4 {
		t.Fatalf("pass: %d deleted, %v, want at least 4", n, err)
	}
	if st := w.state(a); st.Pts != beforeA.Pts+2 {
		t.Errorf("sender's pts %d -> %d, want +2", beforeA.Pts, st.Pts)
	}
	if st := w.state(b); st.Pts != beforeB.Pts+2 {
		t.Errorf("recipient's pts %d -> %d, want +2", beforeB.Pts, st.Pts)
	}
	for _, m := range alive(w.messages(a)) {
		if m.Text == "goes" || m.Text == "goes too" {
			t.Errorf("message %q survived", m.Text)
		}
	}
	if w.dialog(a, mtproto.PEER_USER, b).TopMessage == 0 {
		t.Error("the dialog lost the messages before the period")
	}
	w.checkBox(a)
	w.checkBox(b)

	// A second pass, or a second instance, finds nothing.
	if n, err = w.d.ExpireDue(ctx, time.Now().Unix()+testTTL+100); err != nil || n != 0 {
		t.Fatalf("second pass: %d, %v", n, err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM message_expiry WHERE user_id IN (?, ?)", a, b); n != 0 {
		t.Errorf("rows left: %d", n)
	}
}

// Setting the period the chat has is CHAT_NOT_MODIFIED and writes nothing.
func TestAutoDeleteNotModified(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	if _, err := w.setTTL(a, mtproto.PEER_USER, b, testTTL); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	before := w.state(a)
	if _, err := w.setTTL(a, mtproto.PEER_USER, b, testTTL); err == nil || err.Error() == "" {
		t.Fatalf("same period: %v", err)
	}
	if st := w.state(a); st.Pts != before.Pts {
		t.Errorf("pts moved by a period that did not change: %d -> %d", before.Pts, st.Pts)
	}
}

// The default period of the account applies to its first message of a new
// chat, with auto_setting_from, before that message; and only then.
func TestAutoDeleteDefaultPeriod(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	w.users.defaultTTL = map[int64]int32{a: testTTL}

	r := w.send(a, mtproto.PEER_USER, b, "first")
	var service, text *mtproto.Message
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() != mtproto.Predicate_updateNewMessage {
			continue
		}
		if m := u.GetMessage_MESSAGE(); historyttl.IsChange(m) {
			service = m
		} else {
			text = m
		}
	}
	if service == nil || text == nil {
		t.Fatalf("updates %v", r.GetUpdates())
	}
	if service.GetAction().GetAutoSettingFrom().GetValue() != a || service.GetId() >= text.GetId() {
		t.Errorf("service message %v", service)
	}
	if text.GetTtlPeriod().GetValue() != testTTL {
		t.Errorf("ttl of the first message: %v", text.GetTtlPeriod())
	}
	w.pub.drain()
	if got := w.dialogTTL(b, mtproto.PEER_USER, a); got != testTTL {
		t.Errorf("recipient's period %d", got)
	}

	r = w.send(a, mtproto.PEER_USER, b, "second")
	for _, u := range r.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil && historyttl.IsChange(m) {
			t.Errorf("a second service message: %v", m)
		}
	}
}

// A channel's messages go the same way, one row of its journal for all of
// them, and the worker's pass repeats nothing.
func TestAutoDeleteChannel(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	owner := newUser()
	channelId := int64(90_000_000) + itIdSeq.Add(1)
	now := time.Now().Unix()

	if _, err := w.d.RunBox(ctx, owner, nil, func(b *userbox.Box) error {
		ch, err := channelbox.Create(b, channelbox.NewChannel{Id: channelId, CreatorUserId: owner, Title: "c", Megagroup: true, Date: now})
		if err != nil {
			return err
		}
		if err = ch.SetTTL(testTTL); err != nil {
			return err
		}
		_, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, state, date) VALUES (?, ?, 2, 0, ?)", channelId, owner, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	send := func(text string) []dao.ChannelSent {
		var sent []dao.ChannelSent
		if _, err := w.d.RunBox(ctx, owner, nil, func(b *userbox.Box) error {
			var err error
			sent, _, _, err = w.d.SendChannel(b, channelId, []dao.Outgoing{{RandomId: randomId(), Message: textMessage(owner, mtproto.MakePeerChannel(channelId), text)}}, "")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return sent
	}
	first := send("one")
	if first[0].Message.GetTtlPeriod().GetValue() != testTTL {
		t.Fatalf("channel message ttl: %v", first[0].Message.GetTtlPeriod())
	}
	last := send("two")

	var pts int32
	if err := w.check.QueryRow("SELECT pts FROM channels WHERE id = ?", channelId).Scan(&pts); err != nil {
		t.Fatal(err)
	}
	// Earlier tests of the world's database may have rows due as well.
	n, err := w.d.ExpireDue(ctx, now+testTTL+100)
	if err != nil || n < 2 {
		t.Fatalf("pass: %d, %v", n, err)
	}
	var (
		after int32
		top   int32
	)
	if err = w.check.QueryRow("SELECT pts, top_message FROM channels WHERE id = ?", channelId).Scan(&after, &top); err != nil {
		t.Fatal(err)
	}
	if after != pts+2 {
		t.Errorf("channel pts %d -> %d, want +2", pts, after)
	}
	if top >= last[0].Message.GetId() {
		t.Errorf("top message %d after its messages went", top)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN (?, ?)", channelId, first[0].Message.GetId(), last[0].Message.GetId()); n != 0 {
		t.Errorf("%d messages left", n)
	}
	if n, err = w.d.ExpireDue(ctx, now+testTTL+100); err != nil || n != 0 {
		t.Fatalf("second pass: %d, %v", n, err)
	}
}

// A draft of a new chat keeps a hidden row: the chat still counts as new.
func TestDraftRowIsNoChat(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	if _, err := w.check.Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_type, draft_message_data, date2, deleted) VALUES (?, ?, ?, ?, 2, '{}', 1, 1)",
		a, mtproto.PEER_USER, b, mtproto.MakePeerDialogId(mtproto.PEER_USER, b)); err != nil {
		t.Fatal(err)
	}
	if has, err := w.d.HasPrivateChat(ctx, a, b); err != nil || has {
		t.Errorf("HasPrivateChat of a draft: %v, %v", has, err)
	}
	// The first message shows the dialog and leaves the draft to clear_draft.
	w.send(a, mtproto.PEER_USER, b, "hi")
	if has, err := w.d.HasPrivateChat(ctx, a, b); err != nil || !has {
		t.Errorf("HasPrivateChat after a message: %v, %v", has, err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND draft_type = 2 AND deleted = 0", a, mtproto.PEER_USER, b); n != 1 {
		t.Errorf("the draft went with the message: %d rows", n)
	}
}
