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

package secretchat

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

var (
	alice = Device{UserId: 1, AuthKey: 1001}
	bob1  = Device{UserId: 2, AuthKey: 2001}
	bob2  = Device{UserId: 2, AuthKey: 2002}
)

func newTestStore(ctx context.Context, t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	m, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Terminate(ctx) })
	db, err := userbox.Open(m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, d := range []Device{alice, bob1, bob2} {
		if _, err = db.Exec("INSERT INTO auth_users (auth_key_id, user_id, hash, deleted) VALUES (?, ?, ?, 0)", d.AuthKey, d.UserId, d.AuthKey); err != nil {
			t.Fatal(err)
		}
	}
	return New(db), db
}

func request(ctx context.Context, t *testing.T, s *Store, randomId int32) *Chat {
	t.Helper()
	c, _, err := s.Request(ctx, Request{Admin: alice, ParticipantId: bob1.UserId, RandomId: randomId, GA: []byte{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func accept(ctx context.Context, t *testing.T, s *Store, c *Chat, d Device) *Chat {
	t.Helper()
	a, _, err := s.Accept(ctx, Accept{Participant: d, ChatId: c.Id, AccessHash: c.AccessHash, GB: []byte{4, 5}, KeyFingerprint: 77})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRequestAndAccept(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)

	c, created, err := s.Request(ctx, Request{Admin: alice, ParticipantId: bob1.UserId, RandomId: 11, GA: []byte{1, 2, 3}})
	if err != nil || !created || c.State != StateRequested {
		t.Fatalf("request: %v %v %v", c, created, err)
	}
	again, created, err := s.Request(ctx, Request{Admin: alice, ParticipantId: bob1.UserId, RandomId: 11, GA: []byte{1, 2, 3}})
	if err != nil || created || again.Id != c.Id {
		t.Fatalf("repeated request: %v %v %v", again, created, err)
	}
	if _, _, err = s.Request(ctx, Request{Admin: alice, ParticipantId: bob1.UserId, RandomId: 11, GA: []byte{9}}); !errors.Is(err, ErrRandomIdUsed) {
		t.Fatalf("another request with the random id: %v", err)
	}

	if _, _, err = s.Accept(ctx, Accept{Participant: bob1, ChatId: c.Id, AccessHash: c.AccessHash + 1, GB: []byte{4}}); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("a wrong access hash: %v", err)
	}
	if _, _, err = s.Accept(ctx, Accept{Participant: alice, ChatId: c.Id, AccessHash: c.AccessHash, GB: []byte{4}}); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("the requester accepts: %v", err)
	}
	a := accept(ctx, t, s, c, bob1)
	if a.State != StateActive || a.ParticipantAuthKey != bob1.AuthKey || a.KeyFingerprint != 77 {
		t.Fatalf("accepted: %+v", a)
	}
	again2, repeated, err := s.Accept(ctx, Accept{Participant: bob1, ChatId: c.Id, AccessHash: c.AccessHash, GB: []byte{4, 5}, KeyFingerprint: 77})
	if err != nil || !repeated || again2.State != StateActive {
		t.Fatalf("repeated accept: %v %v %v", again2, repeated, err)
	}
	if _, _, err = s.Accept(ctx, Accept{Participant: bob2, ChatId: c.Id, AccessHash: c.AccessHash, GB: []byte{6}}); !errors.Is(err, ErrAlreadyAccepted) {
		t.Fatalf("a second device: %v", err)
	}
}

// TestSend: the queue numbers per authorization, a repeated random_id is
// answered without a second delivery, only the two ends can send.
func TestSend(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)

	send := func(from Device, randomId int64) (*Sent, error) {
		return s.Send(ctx, Send{From: from, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: randomId, Data: []byte("ciphertext")})
	}
	first, err := send(alice, 100)
	if err != nil || first.Delivered == nil || first.Delivered.Qts != 1 || first.To != bob1 {
		t.Fatalf("send: %+v %v", first, err)
	}
	second, err := send(alice, 101)
	if err != nil || second.Delivered == nil || second.Delivered.Qts != 2 {
		t.Fatalf("send: %+v %v", second, err)
	}
	repeat, err := send(alice, 100)
	if err != nil || repeat.Delivered != nil || repeat.Date != first.Date {
		t.Fatalf("repeat: %+v %v", repeat, err)
	}
	back, err := send(bob1, 200)
	if err != nil || back.Delivered.Qts != 1 || back.To != alice {
		t.Fatalf("back: %+v %v", back, err)
	}
	if _, err = send(bob2, 300); !errors.Is(err, ErrWrongDevice) {
		t.Fatalf("another device of the participant: %v", err)
	}
	if _, err = s.Send(ctx, Send{From: Device{UserId: 9, AuthKey: 9}, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("a stranger: %v", err)
	}
}

// TestDifference: pages of the queue up to the qts, the qts of the request
// acknowledges, the state moves only as far as the page; changes come once
// per state change and stop at the side that made them.
func TestDifference(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	for i := range differencePage + 5 {
		if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: int64(1000 + i), Data: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}

	d, err := s.Difference(ctx, DifferenceInput{Device: bob1, Qts: 0, Date: s.unix()})
	if err != nil || len(d.Messages) != differencePage || !d.More || d.Qts != differencePage {
		t.Fatalf("first page: %d messages, more %v, qts %d, %v", len(d.Messages), d.More, d.Qts, err)
	}
	if len(d.Updates) != 0 {
		t.Fatalf("the acceptor is told of its own acceptance: %d", len(d.Updates))
	}
	if d.Messages[0].Qts != 1 {
		t.Fatalf("first message: qts %d", d.Messages[0].Qts)
	}

	d, err = s.Difference(ctx, DifferenceInput{Device: bob1, Qts: d.Qts, Date: s.unix()})
	if err != nil || len(d.Messages) != 5 || d.More || d.Qts != differencePage+5 {
		t.Fatalf("last page: %d messages, more %v, qts %d, %v", len(d.Messages), d.More, d.Qts, err)
	}
	// The qts of the second request confirmed the first page.
	d, err = s.Difference(ctx, DifferenceInput{Device: bob1, Qts: 0, Date: s.unix()})
	if err != nil || len(d.Messages) != 5 {
		t.Fatalf("after the acknowledgement: %d messages, %v", len(d.Messages), err)
	}

	// Alice gets the acceptance from the state of the chat, as a missed push.
	d, err = s.Difference(ctx, DifferenceInput{Device: alice, Date: s.unix()})
	if err != nil || len(d.Updates) != 1 || d.Updates[0].Chat.Id != c.Id || d.Updates[0].ReadAt != 0 {
		t.Fatalf("alice: %v %v", d, err)
	}
	if v := d.Updates[0].Chat.View(alice); v.GetKeyFingerprint() != 77 {
		t.Fatalf("view: %v", v)
	}
}

// TestDifferenceAhead: a client whose qts is past the queue (a restored
// backup) takes the lower qts.
func TestDifferenceAhead(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	d, err := s.Difference(ctx, DifferenceInput{Device: bob1, Qts: 50, Date: s.unix()})
	if err != nil || d.Qts != 1 || d.Empty(50) {
		t.Fatalf("%+v %v", d, err)
	}
}

// TestAckAndRead: messages.receivedQueue returns the random ids once and
// empties the queue; a read goes to the other end and only forward.
func TestAckAndRead(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	for i := range 3 {
		if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: int64(i + 1), Data: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := s.Ack(ctx, bob1, 2)
	if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("ack: %v %v", ids, err)
	}
	if ids, err = s.Ack(ctx, bob1, 2); err != nil || len(ids) != 0 {
		t.Fatalf("ack again: %v %v", ids, err)
	}

	to, changed, err := s.ReadHistory(ctx, bob1, c.Id, c.AccessHash, 500)
	if err != nil || !changed || to != alice {
		t.Fatalf("read: %v %v %v", to, changed, err)
	}
	if _, changed, err = s.ReadHistory(ctx, bob1, c.Id, c.AccessHash, 400); err != nil || changed {
		t.Fatalf("an old read: %v %v", changed, err)
	}
	d, err := s.Difference(ctx, DifferenceInput{Device: alice, Date: s.unix() - 5})
	if err != nil {
		t.Fatal(err)
	}
	var read int32
	for _, u := range d.Updates {
		if u.ReadAt != 0 {
			read = u.ReadMax
		}
	}
	if read != 500 {
		t.Fatalf("read in the difference: %d", read)
	}
}

// TestDiscardKeepsQueue: without the history, what the peer sent before the
// end still reaches a device that was offline.
func TestDiscardKeepsQueue(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Discard(ctx, Discard{By: alice, ChatId: c.Id}); err != nil {
		t.Fatal(err)
	}
	d, err := s.Difference(ctx, DifferenceInput{Device: bob1, Date: s.unix()})
	if err != nil || len(d.Messages) != 1 || d.Messages[0].RandomId != 1 {
		t.Fatalf("difference after the discard: %+v %v", d, err)
	}
}

// TestDiscard: the queue goes with a discard that deletes the history, the
// second discard is refused, sending is refused, and only the other end is
// told.
func TestDiscard(t *testing.T) {
	ctx := context.Background()
	s, db := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Discard(ctx, Discard{By: bob2, ChatId: c.Id}); !errors.Is(err, ErrWrongDevice) {
		t.Fatalf("another device: %v", err)
	}
	dc, wasRequested, err := s.Discard(ctx, Discard{By: bob1, ChatId: c.Id, HistoryDeleted: true})
	if err != nil || wasRequested || dc.State != StateDiscarded || !dc.HistoryDeleted {
		t.Fatalf("discard: %+v %v %v", dc, wasRequested, err)
	}
	targets, err := s.DiscardTargets(ctx, dc, wasRequested)
	if err != nil || len(targets) != 1 || targets[0] != alice {
		t.Fatalf("targets: %+v %v", targets, err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM encrypted_chat_queue WHERE chat_id = ?", c.Id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("queue: %d %v", n, err)
	}
	if _, _, err = s.Discard(ctx, Discard{By: bob1, ChatId: c.Id}); !errors.Is(err, ErrAlreadyDeclined) {
		t.Fatalf("second discard: %v", err)
	}
	if _, err = s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 2, Data: []byte("x")}); !errors.Is(err, ErrDeclined) {
		t.Fatalf("send after: %v", err)
	}
	// The one sent before is still answered.
	if r, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil || r.Delivered != nil {
		t.Fatalf("repeat after: %+v %v", r, err)
	}
}

// TestDiscardRequest: a declined request reaches the requester and the
// participant's other devices.
func TestDiscardRequest(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	c := request(ctx, t, s, 1)
	dc, wasRequested, err := s.Discard(ctx, Discard{By: bob2, ChatId: c.Id})
	if err != nil || !wasRequested {
		t.Fatalf("decline: %v %v", wasRequested, err)
	}
	targets, err := s.DiscardTargets(ctx, dc, wasRequested)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, d := range targets {
		got[d.AuthKey] = true
	}
	if !got[alice.AuthKey] || !got[bob1.AuthKey] || got[bob2.AuthKey] {
		t.Fatalf("targets: %+v", targets)
	}
}

// TestSweep: chats of an authorization that is gone end, its queue goes,
// retention deletes what is old, a second sweeper finds nothing to do.
func TestSweep(t *testing.T) {
	ctx := context.Background()
	s, db := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	if _, err := s.Send(ctx, Send{From: bob1, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE auth_users SET deleted = 1 WHERE auth_key_id = ?", alice.AuthKey); err != nil {
		t.Fatal(err)
	}
	gone, err := s.Sweep(ctx)
	if err != nil || len(gone) != 1 || gone[0].Chat.Id != c.Id || gone[0].Chat.DiscardedBy != 0 {
		t.Fatalf("sweep: %+v %v", gone, err)
	}
	if again, err := s.Sweep(ctx); err != nil || len(again) != 0 {
		t.Fatalf("second sweep: %d %v", len(again), err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM encrypted_chat_devices WHERE auth_key_id = ?", alice.AuthKey).Scan(&n); err != nil || n != 0 {
		t.Fatalf("device row: %d %v", n, err)
	}

	// Retention: a request nobody answers, an old queue row.
	s.now = func() time.Time { return time.Now().Add(-8 * 24 * time.Hour) }
	r, _, err := s.Request(ctx, Request{Admin: bob1, ParticipantId: alice.UserId, RandomId: 9, GA: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	s.now = time.Now
	gone, err = s.Sweep(ctx)
	if err != nil || len(gone) != 1 || gone[0].Chat.Id != r.Id {
		t.Fatalf("retention: %+v %v", gone, err)
	}
}

// TestKeyReused: Android keeps the key on logout, so the next account on it
// has its own queue and qts, and the one that left loses its queue.
func TestKeyReused(t *testing.T) {
	ctx := context.Background()
	s, db := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	next := Device{UserId: 3, AuthKey: bob1.AuthKey}
	if _, err := db.Exec("UPDATE auth_users SET deleted = 1 WHERE auth_key_id = ? AND user_id = ?", bob1.AuthKey, bob1.UserId); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO auth_users (auth_key_id, user_id, hash, deleted) VALUES (?, ?, ?, 0)", next.AuthKey, next.UserId, 3); err != nil {
		t.Fatal(err)
	}
	// Before the sweep too: nothing of the other account is seen.
	qts, err := s.State(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Difference(ctx, DifferenceInput{Device: next, Qts: 0, Date: s.unix()})
	if err != nil || len(d.Messages) != 0 || d.Qts != qts {
		t.Fatalf("difference of the next account: %+v, qts %d, %v", d, qts, err)
	}
	if _, err = s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM encrypted_chat_queue WHERE auth_key_id = ?", bob1.AuthKey).Scan(&n); err != nil || n != 0 {
		t.Fatalf("queue of the account that left: %d %v", n, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM encrypted_chat_devices WHERE auth_key_id = ? AND user_id = ?", next.AuthKey, next.UserId).Scan(&n); err != nil || n != 1 {
		t.Fatalf("device of the next account: %d %v", n, err)
	}
}

// TestSweepLoginAgain: a new login on the key of the same account ends the
// chats of the old one and drops its queue, the row of auth_users being the
// same one.
func TestSweepLoginAgain(t *testing.T) {
	ctx := context.Background()
	s, db := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	if _, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: 1, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE auth_users SET date_created = ? WHERE auth_key_id = ? AND user_id = ?", s.unix()+10, bob1.AuthKey, bob1.UserId); err != nil {
		t.Fatal(err)
	}
	gone, err := s.Sweep(ctx)
	if err != nil || len(gone) != 1 || gone[0].Chat.Id != c.Id {
		t.Fatalf("sweep: %+v %v", gone, err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM encrypted_chat_queue WHERE auth_key_id = ?", bob1.AuthKey).Scan(&n); err != nil || n != 0 {
		t.Fatalf("queue of the old login: %d %v", n, err)
	}
}

// TestFiles: stored with their fingerprint, and the access hash is checked.
func TestFiles(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(ctx, t)
	f := &File{Id: 55, AccessHash: 66, Size: 3 << 30, DcId: 1, KeyFingerprint: 7}
	if err := s.SaveFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, err := s.File(ctx, 55)
	if err != nil || *got != *f {
		t.Fatalf("file: %+v %v", got, err)
	}
	if ok, err := s.CheckFile(ctx, 55, 66); err != nil || !ok {
		t.Fatalf("check: %v %v", ok, err)
	}
	if ok, err := s.CheckFile(ctx, 55, 67); err != nil || ok {
		t.Fatalf("wrong hash: %v %v", ok, err)
	}
	if ok, err := s.CheckFile(ctx, 56, 66); err != nil || ok {
		t.Fatalf("no such file: %v %v", ok, err)
	}
}
