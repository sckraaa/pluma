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
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	msg_helper "github.com/teamgram/teamgram-server/app/messenger/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// schedule schedules the texts as one send and returns the reply.
func (w *world) schedule(from int64, peerType int32, peerId int64, date int32, texts ...string) (*mtproto.Updates, error) {
	w.t.Helper()
	peer := mtproto.MakePeer(peerType, peerId)
	var list []*msg.OutboxMessage
	for _, s := range texts {
		o := outbox(textMessage(from, peer, s))
		o.ScheduleDate = wrapperspb.Int32(date)
		list = append(list, o)
	}
	return w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: from, AuthKeyId: from * 10, PeerType: peerType, PeerId: peerId, Message: list,
	})
}

func (w *world) mustSchedule(from int64, peerType int32, peerId int64, date int32, texts ...string) []int32 {
	w.t.Helper()
	r, err := w.schedule(from, peerType, peerId, date, texts...)
	if err != nil {
		w.t.Fatalf("schedule from %d: %v", from, err)
	}
	var ids []int32
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewScheduledMessage {
			ids = append(ids, u.GetMessage_MESSAGE().GetId())
		}
	}
	if len(ids) != len(texts) {
		w.t.Fatalf("schedule from %d: reply %v", from, r.GetUpdates())
	}
	return ids
}

// makeDue moves the rows of the ids into the past.
func (w *world) makeDue(ids ...int32) {
	w.t.Helper()
	args := []any{time.Now().Unix() - 1}
	marks := make([]string, len(ids))
	for i, id := range ids {
		args = append(args, id)
		marks[i] = "?"
	}
	if _, err := itCheck.Exec("UPDATE scheduled_messages SET schedule_date = ?, claim_until = 0 WHERE id IN ("+strings.Join(marks, ", ")+")", args...); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) queued(userId int64) int {
	w.t.Helper()
	var n int
	if err := itCheck.QueryRow("SELECT COUNT(*) FROM scheduled_messages WHERE sender_user_id = ?", userId).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// waitQueue waits for the queue of userId to empty.
func (w *world) waitQueue(userId int64) {
	w.t.Helper()
	for deadline := time.Now().Add(30 * time.Second); w.queued(userId) > 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			w.t.Fatalf("%d messages of %d still queued", w.queued(userId), userId)
		}
	}
}

func (w *world) startWorkers(n int) {
	w.t.Helper()
	workers := make([]interface{ Stop() }, n)
	for i := range workers {
		s := msg_helper.NewScheduledWorker(w.d, nil)
		s.Start()
		workers[i] = s
	}
	w.t.Cleanup(func() {
		for _, s := range workers {
			s.Stop()
		}
	})
}

// sentCount is how many messages row of text the sender holds.
func (w *world) sentCount(userId int64, text string) int {
	w.t.Helper()
	var n int
	if err := itCheck.QueryRow("SELECT COUNT(*) FROM messages WHERE user_id = ? AND sender_user_id = ? AND message = ? AND deleted = 0", userId, userId, text).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// Three workers on the same table: every due message goes out exactly once,
// with from_scheduled for the sender and without for the recipient, and the
// sender's devices see the queue go.
func TestScheduledDeliveredOnce(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	texts := make([]string, 30)
	for i := range texts {
		texts[i] = fmt.Sprintf("scheduled %d", i)
	}
	date := int32(time.Now().Unix() + 3600)
	ids := w.mustSchedule(a, mtproto.PEER_USER, b, date, texts...)
	if w.queued(a) != len(texts) {
		t.Fatalf("queued %d, want %d", w.queued(a), len(texts))
	}
	w.makeDue(ids...)
	w.startWorkers(3)
	w.waitQueue(a)
	for _, s := range texts {
		if n := w.sentCount(a, s); n != 1 {
			t.Errorf("%q sent %d times", s, n)
		}
	}
	w.pub.drain()

	var flagged int
	for _, j := range w.journal(a) {
		if j.update(t).GetMessage_MESSAGE().GetFromScheduled() {
			flagged++
		}
	}
	if flagged != len(texts) {
		t.Errorf("%d sent messages with from_scheduled in the sender's journal, want %d", flagged, len(texts))
	}
	for _, j := range w.journal(b) {
		if j.update(t).GetMessage_MESSAGE().GetFromScheduled() {
			t.Errorf("the recipient's update carries from_scheduled: %s", j.Data)
		}
	}
	deletes := 0
	for _, p := range itDelivery.pushesTo(a) {
		for _, u := range p.updates.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateDeleteScheduledMessages {
				if len(u.GetSentMessages()) != len(u.GetMessages()) {
					t.Errorf("updateDeleteScheduledMessages without the sent ids: %v", u)
				}
				deletes += len(u.GetMessages())
			}
		}
	}
	if deletes != len(texts) {
		t.Errorf("updateDeleteScheduledMessages for %d messages, want %d", deletes, len(texts))
	}
}

// A row held by another worker's lease waits for the lease to end: the crash
// of a worker costs one lease.
func TestScheduledLeaseOfADeadWorker(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	ids := w.mustSchedule(a, mtproto.PEER_USER, b, int32(time.Now().Unix()+3600), "lease")
	w.makeDue(ids...)
	if _, err := itCheck.Exec("UPDATE scheduled_messages SET claim_until = ? WHERE id = ?", time.Now().Unix()+60, ids[0]); err != nil {
		t.Fatal(err)
	}
	w.startWorkers(1)
	time.Sleep(3 * time.Second)
	if w.queued(a) != 1 || w.sentCount(a, "lease") != 0 {
		t.Fatalf("delivered under another worker's lease: queued %d, sent %d", w.queued(a), w.sentCount(a, "lease"))
	}
	if _, err := itCheck.Exec("UPDATE scheduled_messages SET claim_until = 0 WHERE id = ?", ids[0]); err != nil {
		t.Fatal(err)
	}
	w.waitQueue(a)
	if n := w.sentCount(a, "lease"); n != 1 {
		t.Fatalf("sent %d times after the lease", n)
	}
}

// A schedule repeated with the same random ids gets the first answer; the
// queue holds no more than the clients are told.
func TestScheduledLimits(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	now := int32(time.Now().Unix())

	if _, err := w.schedule(a, mtproto.PEER_USER, b, now+400*86400, "too late"); err == nil || !strings.Contains(err.Error(), "SCHEDULE_DATE_TOO_LATE") {
		t.Errorf("a date too far: %v", err)
	}
	if r, err := w.schedule(a, mtproto.PEER_USER, b, now+2, "near"); err != nil || len(sentIds(r)) != 1 {
		t.Errorf("a near date sends at once: %v, %v", r, err)
	}

	texts := make([]string, 100)
	for i := range texts {
		texts[i] = fmt.Sprintf("q%d", i)
	}
	w.mustSchedule(a, mtproto.PEER_USER, b, now+3600, texts...)
	if _, err := w.schedule(a, mtproto.PEER_USER, b, now+3600, "one more"); err == nil || !strings.Contains(err.Error(), "SCHEDULE_TOO_MUCH") {
		t.Errorf("past the limit: %v", err)
	}

	c, d := newUser(), newUser()
	o := outbox(textMessage(c, mtproto.MakePeerUser(d), "repeat"))
	o.ScheduleDate = wrapperspb.Int32(now + 3600)
	first, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{UserId: c, AuthKeyId: 1, PeerType: mtproto.PEER_USER, PeerId: d, Message: []*msg.OutboxMessage{o}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{UserId: c, AuthKeyId: 1, PeerType: mtproto.PEER_USER, PeerId: d, Message: []*msg.OutboxMessage{o}})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetUpdates()[1].GetMessage_MESSAGE().GetId() != second.GetUpdates()[1].GetMessage_MESSAGE().GetId() {
		t.Errorf("a repeat made another message: %v, %v", first.GetUpdates(), second.GetUpdates())
	}
	if w.queued(c) != 1 {
		t.Errorf("queued %d after a repeat", w.queued(c))
	}
}

// The queue is read, edited and deleted by the RPCs, and the hash answers
// when nothing changed.
func TestScheduledReadEditDelete(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	ctx := context.Background()
	date := int32(time.Now().Unix() + 3600)
	ids := w.mustSchedule(a, mtproto.PEER_USER, b, date, "one", "two")

	h, err := w.rpc.MsgGetScheduledHistory(ctx, &msg.TLMsgGetScheduledHistory{UserId: a, PeerType: mtproto.PEER_USER, PeerId: b})
	if err != nil || len(h.GetMessages()) != 2 {
		t.Fatalf("history: %v, %v", h, err)
	}
	if h.GetMessages()[0].GetId() != ids[1] || h.GetMessages()[0].GetDate() != date {
		t.Errorf("order or date: %v", h.GetMessages())
	}
	// The second call answers messagesNotModified for the hash.
	var hash uint64
	update := func(v int64) { hash ^= hash >> 21; hash ^= hash << 35; hash ^= hash >> 4; hash += uint64(v) }
	for _, m := range h.GetMessages() {
		update(int64(m.GetId()))
		update(0)
		update(int64(m.GetDate()))
	}
	nm, err := w.rpc.MsgGetScheduledHistory(ctx, &msg.TLMsgGetScheduledHistory{UserId: a, PeerType: mtproto.PEER_USER, PeerId: b, Hash: int64(hash)})
	if err != nil || nm.GetPredicateName() != mtproto.Predicate_messages_messagesNotModified {
		t.Errorf("not modified: %v, %v", nm, err)
	}

	edit := msg.MakeTLOutboxMessage(&msg.OutboxMessage{Message: textMessage(a, mtproto.MakePeerUser(b), "edited"), ScheduleDate: wrapperspb.Int32(date + 60)}).To_OutboxMessage()
	r, err := w.rpc.MsgEditMessageV2(ctx, &msg.TLMsgEditMessageV2{UserId: a, AuthKeyId: 1, PeerType: mtproto.PEER_USER, PeerId: b, EditType: msg.EditTypeNormal,
		NewMessage: edit, DstMessage: &mtproto.MessageBox{MessageId: ids[0]}})
	if err != nil || len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateNewScheduledMessage ||
		r.GetUpdates()[0].GetMessage_MESSAGE().GetDate() != date+60 || !r.GetUpdates()[0].GetMessage_MESSAGE().GetEditHide() {
		t.Fatalf("edit: %v, %v", r, err)
	}
	if _, err = w.rpc.MsgEditMessageV2(ctx, &msg.TLMsgEditMessageV2{UserId: a, AuthKeyId: 1, PeerType: mtproto.PEER_USER, PeerId: b, EditType: msg.EditTypeNormal,
		NewMessage: edit, DstMessage: &mtproto.MessageBox{MessageId: ids[0] + 1000}}); err == nil || !strings.Contains(err.Error(), "MESSAGE_ID_INVALID") {
		t.Errorf("edit of a missing message: %v", err)
	}

	if r, err = w.rpc.MsgDeleteScheduledMessages(ctx, &msg.TLMsgDeleteScheduledMessages{UserId: a, AuthKeyId: 1, PeerType: mtproto.PEER_USER, PeerId: b, Id: ids}); err != nil ||
		r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateDeleteScheduledMessages || len(r.GetUpdates()[0].GetSentMessages()) != 0 {
		t.Fatalf("delete: %v, %v", r, err)
	}
	if w.queued(a) != 0 {
		t.Errorf("queued after the delete: %d", w.queued(a))
	}
}

// A sender out of the group by the time comes loses the message: the checks
// of the send are made when it goes out.
func TestScheduledDroppedWhenSenderLeft(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b}
	ids := w.mustSchedule(a, mtproto.PEER_CHAT, chat, int32(time.Now().Unix()+3600), "left")
	w.chats.members[chat] = []int64{b}
	w.makeDue(ids...)
	w.startWorkers(1)
	w.waitQueue(a)
	if n := w.sentCount(a, "left"); n != 0 {
		t.Fatalf("sent %d times", n)
	}
	w.pub.drain()
	found := false
	for _, p := range itDelivery.pushesTo(a) {
		for _, u := range p.updates.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateDeleteScheduledMessages && len(u.GetSentMessages()) == 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("the sender is not told the message is gone")
	}
}

// An album goes out as one send: one update takes all of it off the queue.
func TestScheduledAlbumDeliveredTogether(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	peer := mtproto.MakePeerUser(b)
	var list []*msg.OutboxMessage
	for _, s := range []string{"album 0", "album 1", "album 2"} {
		o := outbox(textMessage(a, peer, s))
		o.Message.GroupedId = wrapperspb.Int64(int64(a) + 77)
		o.ScheduleDate = wrapperspb.Int32(int32(time.Now().Unix() + 3600))
		list = append(list, o)
	}
	r, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: b, Message: list})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int32
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewScheduledMessage {
			ids = append(ids, u.GetMessage_MESSAGE().GetId())
		}
	}
	w.makeDue(ids...)
	w.startWorkers(1)
	w.waitQueue(a)
	w.pub.drain()
	var deletes [][]int32
	for _, p := range itDelivery.pushesTo(a) {
		for _, u := range p.updates.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateDeleteScheduledMessages {
				deletes = append(deletes, u.GetMessages())
			}
		}
	}
	if len(deletes) != 1 || len(deletes[0]) != len(ids) {
		t.Errorf("the album left the queue in %v, want one update of %d", deletes, len(ids))
	}
}

// Messages waiting for a peer are read page by page, so those that wait
// longest do not hide the others.
func TestScheduledWhenOnlinePages(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	ids := w.mustSchedule(a, mtproto.PEER_USER, b, int32(time.Now().Unix()+3600), "p0", "p1", "p2")
	if _, err := itCheck.Exec("UPDATE scheduled_messages SET schedule_date = ? WHERE sender_user_id = ?", dao.ScheduleWhenOnline, a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := itCheck.Exec("DELETE FROM scheduled_messages WHERE sender_user_id = ?", a); err != nil {
			t.Error(err)
		}
	})
	now := time.Now().Unix()
	afterId := ids[0] - 1
	for i, want := range [][]int32{ids[:2], ids[2:], nil} {
		rows, err := w.d.UnclaimedWhenOnline(context.Background(), now, afterId, 2)
		if err != nil {
			t.Fatal(err)
		}
		got := dao.ScheduledIds(rows)
		if !slices.Equal(got, want) {
			t.Fatalf("page %d: %v, want %v", i, got, want)
		}
		if len(got) > 0 {
			afterId = got[len(got)-1]
		}
	}
}

// A reminder in Saved Messages leaves the dialog unread.
func TestScheduledReminderUnread(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	ids := w.mustSchedule(a, mtproto.PEER_USER, a, int32(time.Now().Unix()+3600), "remember")
	w.makeDue(ids...)
	w.startWorkers(1)
	w.waitQueue(a)
	if d := w.dialog(a, mtproto.PEER_USER, a); d.UnreadCount != 1 {
		t.Errorf("unread %d, want 1", d.UnreadCount)
	}
}
