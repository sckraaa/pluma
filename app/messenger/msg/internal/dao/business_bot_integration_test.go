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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/qtsqueue"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// What the connected bot of Telegram Business is told of the chats it
// serves (task 105, part 7): the owner's box transactions write the events,
// the bot's box transaction queues each update with the next qts of every
// authorization of the bot (qts_updates) and pushes it.

// connectBot connects bot to owner for every chat, with the right to reply.
func (w *world) connectBot(owner, bot int64) *business.Connection {
	w.t.Helper()
	var c *business.Connection
	if err := w.d.Mysql.DB.Transact(context.Background(), func(tx *sqlx.Tx) error {
		var err error
		c, _, err = business.ConnectTx(tx, owner, bot, business.RightReply,
			business.Recipients{ExistingChats: true, NewChats: true}, int32(time.Now().Unix()))
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return c
}

// signIn adds signed-in authorizations of userId on keys.
func (w *world) signIn(userId int64, keys ...int64) {
	w.t.Helper()
	for _, k := range keys {
		if _, err := itCheck.Exec("INSERT INTO auth_users (auth_key_id, user_id, hash, deleted) VALUES (?, ?, ?, 0)", k, userId, k); err != nil {
			w.t.Fatal(err)
		}
	}
}

type queuedRow struct {
	qts    int32
	update *mtproto.Update
}

// qtsQueue is the queue of the authorization key of userId, by qts.
func (w *world) qtsQueue(userId, key int64) []queuedRow {
	w.t.Helper()
	rows, err := itCheck.Query("SELECT qts, update_data FROM qts_updates WHERE auth_key_id = ? AND user_id = ? ORDER BY qts", key, userId)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []queuedRow
	for rows.Next() {
		var (
			r    queuedRow
			data []byte
		)
		if err = rows.Scan(&r.qts, &data); err != nil {
			w.t.Fatal(err)
		}
		if err = jsonx.Unmarshal(data, &r.update); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func TestBusinessBotUpdates(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, peer, bot := newUser(), newUser(), newUser()
	w.users.mu.Lock()
	w.users.bots[bot] = true
	w.users.mu.Unlock()
	w.signIn(bot, bot*10, bot*10+1)
	conn := w.connectBot(owner, bot)

	w.send(peer, mtproto.PEER_USER, owner, "hello shop")
	w.pub.drain()
	r := w.send(owner, mtproto.PEER_USER, peer, "we are open")
	w.pub.drain()
	answer := sentIds(r)[0]

	// Paused in the chat, the bot is told nothing of it.
	if err := w.d.Mysql.DB.Transact(context.Background(), func(tx *sqlx.Tx) error {
		return business.SetPausedTx(tx, owner, peer, true, int32(time.Now().Unix()))
	}); err != nil {
		t.Fatal(err)
	}
	w.send(peer, mtproto.PEER_USER, owner, "are you there")
	w.pub.drain()
	if err := w.d.Mysql.DB.Transact(context.Background(), func(tx *sqlx.Tx) error {
		return business.SetPausedTx(tx, owner, peer, false, int32(time.Now().Unix()))
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := w.rpc.MsgDeleteMessages(context.Background(), &msg.TLMsgDeleteMessages{
		UserId: owner, AuthKeyId: owner * 10, PeerType: mtproto.PEER_EMPTY, Revoke: true, Id: []int32{answer},
	}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	for _, key := range []int64{bot * 10, bot*10 + 1} {
		q := w.qtsQueue(bot, key)
		if len(q) != 3 {
			t.Fatalf("key %d: %d updates queued, want 3: %v", key, len(q), q)
		}
		for i, row := range q {
			if row.qts != qtsqueue.FirstQts+1+int32(i) || row.update.GetQts() != row.qts || row.update.GetConnectionId() != conn.ConnectionID {
				t.Errorf("key %d: update %d with qts %d (in the update %d), connection %q", key, i, row.qts, row.update.GetQts(), row.update.GetConnectionId())
			}
		}
		if u := q[0].update; u.GetPredicateName() != mtproto.Predicate_updateBotNewBusinessMessage ||
			u.GetMessage_MESSAGE().GetMessage() != "hello shop" || u.GetMessage_MESSAGE().GetOut() {
			t.Errorf("key %d: the message of the peer: %v", key, u)
		}
		if u := q[1].update; u.GetPredicateName() != mtproto.Predicate_updateBotNewBusinessMessage ||
			u.GetMessage_MESSAGE().GetMessage() != "we are open" || !u.GetMessage_MESSAGE().GetOut() || u.GetMessage_MESSAGE().GetId() != answer {
			t.Errorf("key %d: the answer of the owner: %v", key, u)
		}
		if u := q[2].update; u.GetPredicateName() != mtproto.Predicate_updateBotDeleteBusinessMessage ||
			u.GetPeer_PEER().GetUserId() != peer || len(u.GetMessages()) != 1 || u.GetMessages()[0] != answer {
			t.Errorf("key %d: the deletion: %v", key, u)
		}
	}
	// Each authorization had each update pushed alone, with its qts and
	// the users it names.
	for _, key := range []int64{bot * 10, bot*10 + 1} {
		var got []int32
		for _, p := range w.pub.delivery.pushesTo(bot) {
			if p.authKey != key {
				continue
			}
			if len(p.updates.GetUpdates()) != 1 {
				t.Fatalf("key %d: a push of %d updates", key, len(p.updates.GetUpdates()))
			}
			u := p.updates.GetUpdates()[0]
			got = append(got, u.GetQts())
			if from := u.GetMessage_MESSAGE().GetFromId().GetUserId(); from != 0 &&
				!slices.ContainsFunc(p.updates.GetUsers(), func(x *mtproto.User) bool { return x.GetId() == from }) {
				t.Errorf("key %d: the author %d not in the users of a push: %v", key, from, p.updates.GetUsers())
			}
		}
		if len(got) != 3 || got[0] != qtsqueue.FirstQts+1 || got[2] != qtsqueue.FirstQts+3 {
			t.Errorf("key %d: pushed qts %v", key, got)
		}
	}
	// The peer's chat is not the bot's: nothing of it reaches the peer's
	// own queue, nor the owner's.
	if n := len(w.qtsQueue(owner, owner*10)) + len(w.qtsQueue(peer, peer*10)); n != 0 {
		t.Errorf("%d updates queued for people", n)
	}
}

// A bot signed in nowhere gets nothing queued, and the events still apply.
func TestBusinessBotOffline(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, peer, bot := newUser(), newUser(), newUser()
	w.users.mu.Lock()
	w.users.bots[bot] = true
	w.users.mu.Unlock()
	w.connectBot(owner, bot)
	w.send(peer, mtproto.PEER_USER, owner, "hello")
	w.pub.drain()
	var n int
	if err := itCheck.QueryRow("SELECT COUNT(*) FROM qts_updates WHERE user_id = ?", bot).Scan(&n); err != nil || n != 0 {
		t.Fatalf("queued for a bot signed in nowhere: %d, %v", n, err)
	}
}

// A message the connected bot sends for its account (task 105, part 8)
// keeps via_business_bot_id in the account's box, which its updates and
// the bot's updateBotNewBusinessMessage show; the other side's copy is the
// account's message without it.
func TestBusinessBotSentMessageCopies(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, peer, bot := newUser(), newUser(), newUser()
	w.users.mu.Lock()
	w.users.bots[bot] = true
	w.users.mu.Unlock()
	w.connectBot(owner, bot)
	w.signIn(bot, bot*10)

	m := textMessage(owner, mtproto.MakePeerUser(peer), "sent by the bot")
	m.ViaBusinessBotId = wrapperspb.Int64(bot)
	r, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		// The request of the bot runs with the bot's key, which is none of
		// the account's devices.
		UserId: owner, AuthKeyId: bot * 10, PeerType: mtproto.PEER_USER, PeerId: peer, Message: []*msg.OutboxMessage{outbox(m)},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage && u.GetMessage_MESSAGE().GetViaBusinessBotId().GetValue() != bot {
			t.Fatalf("the account's answer without the bot: %v", u.GetMessage_MESSAGE())
		}
	}
	via := func(rows []messageRow) (bool, int) {
		t.Helper()
		var data struct {
			ViaBusinessBotId *struct {
				Value int64 `json:"value"`
			} `json:"via_business_bot_id"`
		}
		for _, row := range rows {
			if row.Text != "sent by the bot" {
				continue
			}
			if err := jsonx.UnmarshalFromString(row.Data, &data); err != nil {
				t.Fatal(err)
			}
			return data.ViaBusinessBotId != nil && data.ViaBusinessBotId.Value == bot, 1
		}
		return false, 0
	}
	if has, n := via(w.messages(owner)); n != 1 || !has {
		t.Fatalf("the account's copy: found %d, via the bot %v", n, has)
	}
	if has, n := via(w.messages(peer)); n != 1 || has {
		t.Fatalf("the other side's copy: found %d, via the bot %v", n, has)
	}
	var found bool
	for _, row := range w.qtsQueue(bot, bot*10) {
		if mm := row.update.GetMessage_MESSAGE(); mm.GetMessage() == "sent by the bot" {
			found = mm.GetViaBusinessBotId().GetValue() == bot
		}
	}
	if !found {
		t.Fatalf("the bot is not told of its message with via_business_bot_id: %v", w.qtsQueue(bot, bot*10))
	}
}
