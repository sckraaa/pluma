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
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// texts are the texts of the user's messages that are not deleted, by id.
func (w *world) texts(userId int64) []string {
	w.t.Helper()
	var out []string
	for _, m := range alive(w.messages(userId)) {
		out = append(out, m.Text)
	}
	return out
}

// TestDeleteHistoryRange: messages.deleteHistory with min_date and max_date
// deletes the messages of those days, both bounds included; with revoke the
// other user of the private chat loses their copies too, without it only
// the user's own box changes.
func TestDeleteHistoryRange(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	tag := fmt.Sprint(randomId())
	var texts []string
	for i := range 6 {
		texts = append(texts, fmt.Sprintf("%s-%d", tag, i))
	}
	w.send(a, mtproto.PEER_USER, b, texts...)
	w.pub.drain()
	// Two messages a day: days 1, 2 and 3.
	const day = 86400
	for i, text := range texts {
		if _, err := w.check.Exec("UPDATE messages SET date2 = ? WHERE user_id IN (?, ?) AND message = ?", int64(day*(1+i/2)), a, b, text); err != nil {
			t.Fatal(err)
		}
	}

	del := func(minDate, maxDate int32, revoke bool) *mtproto.Messages_AffectedHistory {
		t.Helper()
		r, err := w.rpc.MsgDeleteHistoryRange(ctx, &msg.TLMsgDeleteHistoryRange{
			UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: b,
			SavedPeerType: mtproto.PEER_EMPTY, MinDate: minDate, MaxDate: maxDate, Revoke: mtproto.ToBool(revoke),
		})
		if err != nil {
			t.Fatal(err)
		}
		w.pub.drain()
		return r
	}
	if r := del(2*day, 3*day-1, true); r.GetPtsCount() != 2 || r.GetOffset() != 0 || r.GetPts() != w.state(a).Pts {
		t.Errorf("day 2 with revoke: pts %d, pts_count %d, offset %d", r.GetPts(), r.GetPtsCount(), r.GetOffset())
	}
	want := []string{texts[0], texts[1], texts[4], texts[5]}
	if got := w.texts(a); !slices.Equal(got, want) {
		t.Errorf("A keeps %v, want %v", got, want)
	}
	if got := w.texts(b); !slices.Equal(got, want) {
		t.Errorf("B keeps %v, want %v", got, want)
	}

	if r := del(3*day, 3*day, false); r.GetPtsCount() != 2 {
		t.Errorf("day 3 for A: pts_count %d", r.GetPtsCount())
	}
	if got := w.texts(a); !slices.Equal(got, texts[:2]) {
		t.Errorf("A keeps %v, want %v", got, texts[:2])
	}
	if got := w.texts(b); !slices.Equal(got, want) {
		t.Errorf("B keeps %v without revoke, want %v", got, want)
	}
	if r := del(5*day, 6*day, true); r.GetPtsCount() != 0 || r.GetPts() != w.state(a).Pts {
		t.Errorf("no messages in the range: pts %d, pts_count %d", r.GetPts(), r.GetPtsCount())
	}
	for _, u := range []int64{a, b} {
		w.checkBox(u)
	}
}

type savedDialogRow struct {
	Top     int32
	Deleted bool
}

func (w *world) savedDialog(userId int64, peer int64) savedDialogRow {
	w.t.Helper()
	var r savedDialogRow
	err := w.check.QueryRow("SELECT top_message, deleted FROM saved_dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		userId, mtproto.PEER_USER, peer).Scan(&r.Top, &r.Deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return savedDialogRow{Deleted: true}
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

// saveToSelf sends texts to the user's Saved Messages as the sub-chat of
// savedPeer (a forward from that user, or the user's notes) and returns the
// new ids.
func (w *world) saveToSelf(userId, savedPeer int64, texts ...string) []int32 {
	w.t.Helper()
	self := mtproto.MakePeerUser(userId)
	var list []*msg.OutboxMessage
	for _, s := range texts {
		m := textMessage(userId, self, s)
		m.SavedPeerId = mtproto.MakePeerUser(savedPeer)
		list = append(list, outbox(m))
	}
	r, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: userId, AuthKeyId: userId * 10, PeerType: mtproto.PEER_USER, PeerId: userId, Message: list,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return sentIds(r)
}

// TestDeleteSavedHistory: messages.deleteSavedHistory deletes one sub-chat
// of Saved Messages in portions of 1000; the sub-chat leaves the list of
// Saved Messages dialogs with its last message, the others stay. Any
// deletion moves the top message of a sub-chat to the newest one left.
func TestDeleteSavedHistory(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	notes := w.saveToSelf(a, a, "note 1", "note 2")
	texts := make([]string, 50)
	for i := range texts {
		texts[i] = "from B"
	}
	for range 21 {
		w.saveToSelf(a, b, texts...)
	}
	if d := w.savedDialog(a, b); d.Deleted || d.Top == 0 {
		t.Fatalf("the sub-chat of B before: %+v", d)
	}

	var offsets, counts []int32
	for {
		r, err := w.rpc.MsgDeleteHistoryRange(ctx, &msg.TLMsgDeleteHistoryRange{
			UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: a,
			SavedPeerType: mtproto.PEER_USER, SavedPeerId: b, Revoke: mtproto.BoolFalse,
		})
		if err != nil {
			t.Fatal(err)
		}
		offsets, counts = append(offsets, r.GetOffset()), append(counts, r.GetPtsCount())
		if r.GetOffset() == 0 || len(offsets) > 5 {
			break
		}
	}
	if !slices.Equal(offsets, []int32{50, 0}) || !slices.Equal(counts, []int32{1000, 50}) {
		t.Errorf("offsets %v, pts counts %v; want [50 0], [1000 50]", offsets, counts)
	}
	if got := w.texts(a); !slices.Equal(got, []string{"note 1", "note 2"}) {
		t.Errorf("A keeps %v", got)
	}
	if d := w.savedDialog(a, b); !d.Deleted {
		t.Errorf("the sub-chat of B stays: %+v", d)
	}
	if d := w.savedDialog(a, a); d.Deleted || d.Top != notes[1] {
		t.Errorf("the notes: %+v, want top %d", d, notes[1])
	}

	// Deleting the newest note moves the top of the notes to the one left.
	if _, err := w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: a, AuthKeyId: a * 10, Id: notes[1:]}); err != nil {
		t.Fatal(err)
	}
	if d := w.savedDialog(a, a); d.Deleted || d.Top != notes[0] {
		t.Errorf("the notes after deleting the newest: %+v, want top %d", d, notes[0])
	}
	// A new message brings a deleted sub-chat back.
	again := w.saveToSelf(a, b, "from B again")
	if d := w.savedDialog(a, b); d.Deleted || d.Top != again[0] {
		t.Errorf("the sub-chat of B after a new message: %+v", d)
	}
	w.checkBox(a)
}
