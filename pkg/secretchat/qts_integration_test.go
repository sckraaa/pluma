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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/qtsqueue"
)

// TestSharedQts: a message of a secret chat and another update with qts of
// the same authorization take their numbers from one counter, one after
// the other; each queue gives its own, and the state and the sweep of a
// gone authorization cover both.
func TestSharedQts(t *testing.T) {
	ctx := context.Background()
	s, db := newTestStore(ctx, t)
	c := accept(ctx, t, s, request(ctx, t, s, 1), bob1)
	send := func(randomId int64) int32 {
		t.Helper()
		sent, err := s.Send(ctx, Send{From: alice, ChatId: c.Id, AccessHash: c.AccessHash, RandomId: randomId, Data: []byte("ciphertext")})
		if err != nil {
			t.Fatal(err)
		}
		return sent.Delivered.Qts
	}
	other := func() int32 {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		u := mtproto.MakeTLUpdateBotDeleteBusinessMessage(&mtproto.Update{ConnectionId: "c", Peer_PEER: mtproto.MakePeerUser(1), Messages: []int32{1}}).To_Update()
		qts, err := qtsqueue.Append(qtsqueue.SQLTx(ctx, tx), qtsqueue.Device(bob1), u, nil, s.unix())
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return qts
	}

	if x, y, z := send(100), other(), send(101); x != firstQts+1 || y != firstQts+2 || z != firstQts+3 {
		t.Fatalf("numbers %d, %d, %d", x, y, z)
	}
	if qts, err := s.State(ctx, bob1); err != nil || qts != firstQts+3 {
		t.Fatalf("state %d, %v", qts, err)
	}
	d, err := s.Difference(ctx, DifferenceInput{Device: bob1, Qts: firstQts, Date: s.unix()})
	if err != nil || len(d.Messages) != 2 || d.Messages[0].Qts != firstQts+1 || d.Messages[1].Qts != firstQts+3 || d.Qts != firstQts+3 {
		t.Fatalf("secret chats: %+v, %v", d, err)
	}
	q, err := s.Qts().Difference(ctx, qtsqueue.Device(bob1), firstQts, d.Qts)
	if err != nil || len(q.Updates) != 1 || q.Updates[0].Qts != firstQts+2 {
		t.Fatalf("other updates: %+v, %v", q, err)
	}

	// bob1 signs out: the sweep clears the counter and both queues.
	if _, err = db.Exec("UPDATE auth_users SET deleted = 1 WHERE auth_key_id = ? AND user_id = ?", bob1.AuthKey, bob1.UserId); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, table := range []string{"qts_updates", "encrypted_chat_queue", "encrypted_chat_devices"} {
		err = db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE auth_key_id = ? AND user_id = ?", bob1.AuthKey, bob1.UserId).Scan(&n)
		if err != nil && err != sql.ErrNoRows || n != 0 {
			t.Fatalf("%s keeps %d rows of a gone authorization, %v", table, n, err)
		}
	}
}
