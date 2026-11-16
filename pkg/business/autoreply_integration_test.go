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

package business

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The automatic replies on the production MySQL with the whole schema: the
// settings keep the line of new chats, refuse a shortcut that is not the
// account's or is empty, go with their shortcut; the queue holds one row
// per trigger, a claim holds against other workers and counts attempts,
// and the reads of a chat see what the box holds.
func TestAutoReplyStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	s := NewStore(conn)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tx := func(f func(tx *sqlx.Tx) error) error {
		t.Helper()
		return conn.Transact(ctx, f)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n []struct {
			N int `db:"n"`
		}
		must(conn.QueryRowsPartial(ctx, &n, q, args...))
		return n[0].N
	}
	shortcutSeq := int32(0)
	// shortcut makes a shortcut of user holding messages messages.
	shortcut := func(user int64, messages int) int32 {
		t.Helper()
		shortcutSeq++
		name := "s" + string(rune('a'+shortcutSeq))
		res, err := conn.Exec(ctx, "INSERT INTO quick_reply_shortcuts (user_id, shortcut, shortcut_key, sort, created_at) VALUES (?, ?, ?, ?, 0)",
			user, name, name, shortcutSeq)
		must(err)
		id, err := res.LastInsertId()
		must(err)
		for range messages {
			_, err = conn.Exec(ctx, "INSERT INTO quick_reply_messages (user_id, shortcut_id, message_data, date) VALUES (?, ?, '{}', 0)", user, id)
			must(err)
		}
		return int32(id)
	}
	set := func(a *AutoReply, now int32) error {
		return tx(func(tx *sqlx.Tx) error { return SetAutoReplyTx(tx, a, now) })
	}
	read := func(user int64) []*AutoReply {
		t.Helper()
		list, err := s.AutoReplies(ctx, user)
		must(err)
		return list
	}

	const owner, other = 1, 2
	hello, empty, foreign := shortcut(owner, 2), shortcut(owner, 0), shortcut(other, 1)

	if err = set(&AutoReply{UserID: owner, Kind: KindGreeting, ShortcutID: empty, NoActivityDays: 7}, 100); !errors.Is(err, ErrShortcutUnusable) {
		t.Errorf("an empty shortcut: %v", err)
	}
	if err = set(&AutoReply{UserID: owner, Kind: KindGreeting, ShortcutID: foreign, NoActivityDays: 7}, 100); !errors.Is(err, ErrShortcutUnusable) {
		t.Errorf("another account's shortcut: %v", err)
	}
	if len(read(owner)) != 0 {
		t.Fatal("a refused setting was kept")
	}

	greeting := &AutoReply{UserID: owner, Kind: KindGreeting, ShortcutID: hello, NoActivityDays: 7,
		Recipients: Recipients{NewChats: true, Users: []int64{9, 8, 9}}}
	must(set(greeting, 100))
	// A change keeps the line of new chats.
	greeting.NoActivityDays = 14
	must(set(greeting, 200))
	away := &AutoReply{UserID: owner, Kind: KindAway, ShortcutID: hello, Schedule: AwayCustom, ScheduleStart: 10, ScheduleEnd: 20, OfflineOnly: true,
		Recipients: Recipients{ExcludeSelected: true}}
	must(set(away, 300))
	list := read(owner)
	if len(list) != 2 {
		t.Fatalf("%d settings, want 2", len(list))
	}
	g, a := AutoReplyOf(list, KindGreeting), AutoReplyOf(list, KindAway)
	if g.NoActivityDays != 14 || g.EnabledAt != 100 || len(g.Recipients.Users) != 2 || g.Recipients.Users[0] != 8 || !g.Recipients.NewChats {
		t.Errorf("greeting read back %+v", g)
	}
	if a.Schedule != AwayCustom || a.ScheduleStart != 10 || a.ScheduleEnd != 20 || !a.OfflineOnly || !a.Recipients.ExcludeSelected || a.EnabledAt != 300 {
		t.Errorf("away read back %+v", a)
	}

	// The queue: one row per trigger, however often it comes.
	queue := func(q QueuedAutoReply) {
		t.Helper()
		must(tx(func(tx *sqlx.Tx) error { return QueueAutoReplyTx(tx, q, 1000) }))
	}
	q := QueuedAutoReply{UserID: owner, PeerID: 5, Kind: KindGreeting, TriggerBoxID: 11, ShortcutID: hello}
	queue(q)
	queue(q)
	queue(QueuedAutoReply{UserID: owner, PeerID: 5, Kind: KindAway, TriggerBoxID: 11, ShortcutID: hello})
	queue(QueuedAutoReply{UserID: owner, PeerID: 6, Kind: KindAway, TriggerBoxID: 12, ShortcutID: hello})
	if n := count("SELECT COUNT(*) AS n FROM business_auto_replies WHERE user_id = ?", owner); n != 3 {
		t.Fatalf("%d queued, want 3", n)
	}
	must(tx(func(tx *sqlx.Tx) error {
		queued, err := AutoReplyQueuedTx(tx, owner, 5, KindGreeting)
		if err == nil && !queued {
			t.Error("the queued greeting is not seen")
		}
		queued, err = AutoReplyQueuedTx(tx, owner, 7, KindGreeting)
		if err == nil && queued {
			t.Error("a greeting to nobody is seen")
		}
		return err
	}))

	// Claims: two workers at once, one wins; the lease holds; the
	// attempt is counted.
	rows, err := s.UnclaimedAutoReplies(ctx, 5000, 10)
	must(err)
	if len(rows) != 3 {
		t.Fatalf("%d unclaimed, want 3", len(rows))
	}
	first := rows[0]
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.ClaimAutoReply(ctx, first.ID, 5000, 5000+120_000)
			if err != nil {
				t.Error(err)
				return
			}
			if r != nil {
				mu.Lock()
				wins++
				mu.Unlock()
				if r.Attempts != 1 || r.Kind != first.Kind || r.PeerID != first.PeerID {
					t.Errorf("claimed %+v", r)
				}
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d workers claimed the row, want 1", wins)
	}
	if rows, _ = s.UnclaimedAutoReplies(ctx, 6000, 10); len(rows) != 2 {
		t.Errorf("%d unclaimed under a lease, want 2", len(rows))
	}
	r, err := s.ClaimAutoReply(ctx, first.ID, 200_000, 400_000)
	must(err)
	if r == nil || r.Attempts != 2 {
		t.Fatalf("the claim after the lease: %+v", r)
	}

	// Sent: off the queue, in the log.
	must(tx(func(tx *sqlx.Tx) error { return FinishAutoReplyTx(tx, r, 777) }))
	must(tx(func(tx *sqlx.Tx) error {
		sent, err := AutoReplyLastSentTx(tx, owner, r.PeerID, r.Kind)
		if err == nil && sent != 777 {
			t.Errorf("last sent %d, want 777", sent)
		}
		return err
	}))
	if n := count("SELECT COUNT(*) AS n FROM business_auto_replies WHERE id = ?", r.ID); n != 0 {
		t.Error("a sent reply stays queued")
	}
	// A send of a row taken off the queue meanwhile (its setting turned
	// off) is refused, so that its transaction rolls back.
	if err := tx(func(tx *sqlx.Tx) error { return FinishAutoReplyTx(tx, r, 778) }); !errors.Is(err, ErrAutoReplyGone) {
		t.Errorf("finishing a row no longer queued: %v, want ErrAutoReplyGone", err)
	}

	// Turned off: the setting and its queued rows go, the other kind's
	// stay.
	must(tx(func(tx *sqlx.Tx) error { return DeleteAutoReplyTx(tx, owner, KindAway) }))
	if l := read(owner); len(l) != 1 || l[0].Kind != KindGreeting {
		t.Errorf("after the away message was turned off: %+v", l)
	}
	if n := count("SELECT COUNT(*) AS n FROM business_auto_replies WHERE user_id = ? AND kind = ?", owner, KindAway); n != 0 {
		t.Errorf("%d away replies queued after it was turned off", n)
	}

	// A shortcut deleted turns its settings off.
	must(set(away, 400))
	queue(QueuedAutoReply{UserID: owner, PeerID: 6, Kind: KindAway, TriggerBoxID: 13, ShortcutID: hello})
	var off []AutoReplyKind
	must(tx(func(tx *sqlx.Tx) error {
		var err error
		off, err = DisableShortcutTx(tx, owner, hello)
		return err
	}))
	if len(off) != 2 || off[0] != KindGreeting || off[1] != KindAway || len(read(owner)) != 0 {
		t.Errorf("turned off %v, left %+v", off, read(owner))
	}
	if n := count("SELECT COUNT(*) AS n FROM business_auto_replies WHERE user_id = ?", owner); n != 0 {
		t.Errorf("%d replies queued after the shortcut went", n)
	}

	// The chat as the box holds it.
	box := int32(0)
	// message puts a message of sender in the chat of user with partner
	// in user's box at date.
	message := func(user, partner, sender int64, date int64, deleted bool) {
		t.Helper()
		box++
		did := mtproto.MakeDialogId(user, mtproto.PEER_USER, partner)
		_, err := conn.Exec(ctx, "INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id,"+
			" peer_type, peer_id, message_data, message, date2, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'null', '', ?, ?)",
			user, box, did.A, did.B, int64(box), sender, mtproto.PEER_USER, partner, date, deleted)
		must(err)
	}
	history := func(user, peer int64) ChatHistory {
		t.Helper()
		var h ChatHistory
		must(tx(func(tx *sqlx.Tx) error {
			var err error
			h, err = ChatHistoryTx(tx, user, peer)
			return err
		}))
		return h
	}
	wrote := func(user, peer int64, since int32) bool {
		t.Helper()
		var w bool
		must(tx(func(tx *sqlx.Tx) error {
			var err error
			w, err = OwnerWroteSinceTx(tx, user, peer, since)
			return err
		}))
		return w
	}
	if h := history(owner, 20); h.Any {
		t.Errorf("an empty chat: %+v", h)
	}
	message(owner, 20, 20, 1000, true) // the first, deleted
	message(owner, 20, 20, 2000, false)
	message(owner, 20, owner, 3000, false)
	message(owner, 20, 20, 4000, true) // the last, deleted
	message(owner, 21, 21, 9000, false)
	if h := history(owner, 20); !h.Any || h.First != 1000 || h.Last != 4000 {
		t.Errorf("the chat: %+v", h)
	}
	if !wrote(owner, 20, 2500) {
		t.Error("the owner's message at 3000 is not seen after 2500")
	}
	if wrote(owner, 20, 3000) {
		t.Error("a message of the owner seen after 3000")
	}
	if wrote(owner, 21, 0) {
		t.Error("the owner wrote nothing to 21")
	}
}
