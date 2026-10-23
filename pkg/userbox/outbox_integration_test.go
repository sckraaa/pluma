//go:build integration

package userbox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The events of box transactions on MySQL (ADR 0012, pp. 2 and 5).

type storedRow struct {
	id                   int64
	initiator, recipient int64
	topic, method        string
	payload              []byte
	state                EventState
	revision, deferrals  int
	attempts             int
	err                  string
}

func storedRows(t testing.TB, db *sql.DB, where string, args ...any) []storedRow {
	t.Helper()
	rows, err := db.Query("SELECT id, initiator_id, recipient_id, topic, method, payload, state, revision, deferrals, attempts, error FROM mq_outbox WHERE "+where+" ORDER BY id", args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedRow
	for rows.Next() {
		var r storedRow
		var st int8
		if err = rows.Scan(&r.id, &r.initiator, &r.recipient, &r.topic, &r.method, &r.payload, &st, &r.revision, &r.deferrals, &r.attempts, &r.err); err != nil {
			t.Fatal(err)
		}
		r.state = EventState(st)
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// addRow commits an inbox row to recipient in state, as if published.
func addRow(t testing.TB, db *sql.DB, recipient int64, state EventState) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO mq_outbox (initiator_id, topic, recipient_id, method, payload, created_at, state) VALUES (1, 'Inbox-T', ?, 'm', '{}', ?, ?)",
		recipient, time.Now().UnixMilli(), state)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func rowById(t testing.TB, db *sql.DB, id int64) (storedRow, bool) {
	t.Helper()
	rows := storedRows(t, db, "id = ?", id)
	if len(rows) == 0 {
		return storedRow{}, false
	}
	return rows[0], true
}

// take runs a transaction of recipient that takes event id and, when it may,
// applies it: a message, then FinishEvent.
func take(t testing.TB, db *sql.DB, recipient, id int64) TakeOutcome {
	t.Helper()
	var outcome TakeOutcome
	mustRun(t, db, recipient, func(b *Box) error {
		var err error
		if outcome, err = b.TakeEvent(id); err != nil || outcome != TakeApply {
			return err
		}
		if err = send(b); err != nil {
			return err
		}
		return b.FinishEvent()
	})
	return outcome
}

func TestEventsWithTheTransaction(t *testing.T) {
	pools(t, func(t *testing.T, db *sql.DB) {
		u, r1, r2 := itUser(), itUser(), itUser()
		res := mustRun(t, db, u, func(b *Box) error {
			if err := send(b); err != nil {
				return err
			}
			b.Emit(Event{Topic: "Inbox-T", Recipient: r1, Method: "first", Data: []byte(`{"a":1}`)})
			b.Emit(Event{Topic: "Sync-T", Recipient: u, Method: "push", Data: []byte(`{}`)})
			b.Emit(Event{Topic: "Inbox-T", Recipient: r2, Method: "second", Data: []byte(`{"b":2}`)})
			return nil
		})
		rows := storedRows(t, db, "initiator_id = ?", u)
		if res.Emitted != 3 || len(rows) != 3 {
			t.Fatalf("emitted %d, rows %+v", res.Emitted, rows)
		}
		for i, want := range []string{"first", "push", "second"} {
			if rows[i].method != want || rows[i].state != EventPending {
				t.Errorf("row %d: %+v", i, rows[i])
			}
		}
		// An event that cannot be written rolls the whole transaction back.
		_, err := Run(context.Background(), db, u, func(b *Box) error {
			if err := send(b); err != nil {
				return err
			}
			b.Emit(Event{Topic: "Inbox-T", Recipient: r1, Method: "fine", Data: []byte(`{}`)})
			b.Emit(Event{Topic: "Inbox-T", Recipient: r1, Method: "big", Data: make([]byte, MaxEventBytes+1)})
			return nil
		})
		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("error %v", err)
		}
		if got := storedRows(t, db, "initiator_id = ?", u); len(got) != 3 || mustRead(t, db, u).Pts != BasePts+1 {
			t.Error("the refused transaction left rows")
		}
	})
}

// A broadcast of large events to 200 recipients: several statements under
// max_allowed_packet, one transaction.
func TestEventsLargeBroadcast(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 8)
	u := itUser()
	payload := bytes.Repeat([]byte("x"), 100<<10)
	mustRun(t, db, u, func(b *Box) error {
		for i := range 200 {
			b.Emit(Event{Topic: "Inbox-T", Recipient: 50_000_000 + int64(i), Method: fmt.Sprint(i), Data: payload})
		}
		return send(b)
	})
	rows := storedRows(t, db, "initiator_id = ?", u)
	if len(rows) != 200 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if r.method != fmt.Sprint(i) || len(r.payload) != len(payload) {
			t.Fatalf("row %d: %s, %d bytes", i, r.method, len(r.payload))
		}
	}
}

func TestTakeEventOnMySQL(t *testing.T) {
	pools(t, func(t *testing.T, db *sql.DB) {
		t.Run("apply once", func(t *testing.T) {
			r := itUser()
			id := addRow(t, db, r, EventPublished)
			if got := take(t, db, r, id); got != TakeApply {
				t.Fatalf("outcome %v", got)
			}
			if _, ok := rowById(t, db, id); ok {
				t.Error("the row of an applied event is left")
			}
			if got := take(t, db, r, id); got != TakeDuplicate {
				t.Fatalf("again: %v", got)
			}
			if st := mustRead(t, db, r); st.Pts != BasePts+1 {
				t.Errorf("applied twice: pts %d", st.Pts)
			}
		})
		t.Run("in lane order", func(t *testing.T) {
			r := itUser()
			early := addRow(t, db, r, EventPublished)
			late := addRow(t, db, r, EventPublished)
			if got := take(t, db, r, late); got != TakeDeferred {
				t.Fatalf("late first: %v", got)
			}
			row, _ := rowById(t, db, late)
			if row.state != EventPending || row.deferrals != 1 || row.revision != 1 {
				t.Errorf("deferred row %+v", row)
			}
			if row, _ = rowById(t, db, early); row.state != EventPending {
				t.Errorf("earlier row %+v not returned to pending", row)
			}
			if take(t, db, r, early) != TakeApply || take(t, db, r, late) != TakeApply {
				t.Error("not applied in order")
			}
		})
		t.Run("parked", func(t *testing.T) {
			r := itUser()
			dead := addRow(t, db, r, EventDead)
			id := addRow(t, db, r, EventPublished)
			if got := take(t, db, r, id); got != TakeParked {
				t.Fatalf("outcome %v", got)
			}
			if row, _ := rowById(t, db, id); row.state != EventParked {
				t.Errorf("row %+v", row)
			}
			if got := take(t, db, r, dead); got != TakeWaiting {
				t.Errorf("dead row taken: %v", got)
			}
		})
		t.Run("failed", func(t *testing.T) {
			r := itUser()
			id := addRow(t, db, r, EventPublished)
			mustRun(t, db, r, func(b *Box) error {
				if _, err := b.TakeEvent(id); err != nil {
					return err
				}
				return b.FailEvent(errors.New("rpc 400 MESSAGE_ID_INVALID"), 3)
			})
			if row, _ := rowById(t, db, id); row.state != EventDead || row.attempts != 3 || row.err != "rpc 400 MESSAGE_ID_INVALID" {
				t.Errorf("row %+v", row)
			}
		})
	})
}

// Two consumers take the same event at once (a rebalance): the lock of the
// recipient's box orders them, and the second finds no row.
func TestTakeEventConcurrently(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 16)
	r := itUser()
	ids := make([]int64, 20)
	for i := range ids {
		ids[i] = addRow(t, db, r, EventPublished)
	}
	var (
		mu       sync.Mutex
		outcomes = map[TakeOutcome]int{}
	)
	for _, id := range ids {
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				o := take(t, db, r, id)
				mu.Lock()
				outcomes[o]++
				mu.Unlock()
			}()
		}
		wg.Wait()
	}
	if outcomes[TakeApply] != len(ids) || outcomes[TakeDuplicate] != len(ids) {
		t.Errorf("outcomes %v: want each event applied once", outcomes)
	}
	if st := mustRead(t, db, r); st.Pts != BasePts+int32(len(ids)) {
		t.Errorf("pts %d", st.Pts)
	}
	mustCheckJournal(t, db, r)
}

// The statements of taking an event read and change rows by key: they
// never scan the table or lock ranges other transactions insert into.
func TestTakeEventPlans(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 4)
	r := itUser()
	var ids []int64
	for range 50 {
		ids = append(ids, addRow(t, db, r, EventPublished))
	}
	a, b := ids[0], ids[1]
	for _, c := range []struct {
		query string
		args  []any
		key   string
	}{
		{"SELECT topic, recipient_id, state, deferrals, replays FROM mq_outbox WHERE id = ?", []any{a}, "PRIMARY"},
		{"SELECT id, state, replays FROM mq_outbox FORCE INDEX (recipient) WHERE topic = ? AND recipient_id = ? AND id < ? ORDER BY id LIMIT 1", []any{"Inbox-T", r, b}, "recipient"},
		{"SELECT id, state, replays FROM mq_outbox FORCE INDEX (recipient) WHERE topic = ? AND recipient_id = ? AND id < ?", []any{"Inbox-T", r, b}, "recipient"},
		{"UPDATE mq_outbox SET state = ?, revision = revision + 1, deferrals = deferrals + (id = ?) WHERE id IN (?, ?)", []any{0, a, a, b}, "PRIMARY"},
		{"UPDATE mq_outbox SET state = ?, revision = revision + 1 WHERE id = ?", []any{3, a}, "PRIMARY"},
		{"DELETE FROM mq_outbox WHERE id = ?", []any{a}, "PRIMARY"},
	} {
		key := explainKey(t, db, c.query, c.args...)
		if key != c.key {
			t.Errorf("%s: key %q, want %s", c.query, key, c.key)
		}
	}
}

// explainKey returns the key of the plan of query (the first table).
func explainKey(t testing.TB, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, c := range cols {
			if strings.EqualFold(c, "key") && vals[slices.Index(cols, "table")].String == "mq_outbox" {
				keys = append(keys, vals[i].String)
			}
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
