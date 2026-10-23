package userbox

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/proto/mtproto"
)

func newMessage(id int32) *mtproto.Update {
	return mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
		Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Id: id, Message: "hello"}).To_Message(),
	}).To_Update()
}

func deleteMessages(ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDeleteMessages(&mtproto.Update{Messages: ids}).To_Update()
}

func mysqlErr(n uint16) error { return &mysql.MySQLError{Number: n, Message: "injected"} }

// sendMessage is the fn of a typical write: a box id, a row, a journal row,
// an event and a cache key.
func sendMessage(b *Box) error {
	id := b.NextMessageId()
	if _, err := b.Tx().Exec("INSERT INTO messages (user_id, user_message_box_id) VALUES (?, ?)", b.UserId(), id); err != nil {
		return err
	}
	b.AddPtsUpdate(newMessage(id), 1)
	b.Emit(Event{Topic: "Inbox-T", Recipient: 2, Method: "send", Data: []byte(fmt.Sprint(id))})
	b.InvalidateCache(fmt.Sprintf("dialog_%d", id))
	return nil
}

func TestRunAllocatesInOneTransaction(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	ctx := context.Background()

	res, err := Run(ctx, db, 7, func(b *Box) error {
		if got := b.State(); got != (State{Pts: BasePts, Date: b.Date()}) {
			t.Errorf("state of a new box: %+v", got)
		}
		id1 := b.NextMessageId()
		b.AddPtsUpdate(newMessage(id1), 1)
		id2 := b.NextMessageId()
		b.AddPtsUpdate(newMessage(id2), 1)
		b.AddPtsUpdate(deleteMessages(id1, id2), 2)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got [][2]int32
	for _, u := range res.Updates {
		got = append(got, [2]int32{u.Pts_INT32, u.PtsCount})
	}
	if want := [][2]int32{{2, 1}, {3, 1}, {5, 2}}; !slices.Equal(got, want) {
		t.Errorf("updates (pts, pts_count) %v, want %v", got, want)
	}
	if res.State.Pts != 5 || res.State.LastMessageBoxId != 2 {
		t.Errorf("result state %+v, want pts 5, box id 2", res.State)
	}
	if st, _ := s.state(7); st.Pts != 5 || st.LastMessageBoxId != 2 {
		t.Errorf("committed state %+v, want pts 5, box id 2", st)
	}
	rows := s.rows(7)
	if len(rows) != 3 || rows[0].pts != 2 || rows[2].pts != 5 || rows[2].ptsCount != 2 {
		t.Errorf("journal %+v", rows)
	}
	if rows[0].updateType != mtproto.PTS_UPDATE_NEW_MESSAGE || rows[2].updateType != mtproto.PTS_UPDATE_DELETE_MESSAGES {
		t.Errorf("journal update types %d, %d", rows[0].updateType, rows[2].updateType)
	}
	want := []string{fmt.Sprintf("begin %d", sql.LevelReadCommitted), "lock", "select_locked", "journal", "update_state", "commit", "tx_commit"}
	if got := s.statements(); !slices.Equal(got, want) {
		t.Errorf("statements %v, want %v", got, want)
	}

	// The next transaction continues from the committed state; its event
	// is the last statement before COMMIT.
	res, err = Run(ctx, db, 7, sendMessage)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.statements()[len(want):]; !slices.Equal(got[len(got)-4:], []string{"update_state", "events", "commit", "tx_commit"}) {
		t.Errorf("statements %v: want the events after the state, right before COMMIT", got)
	}
	if u := res.Updates[0]; u.Pts_INT32 != 6 || u.PtsCount != 1 || u.Message_MESSAGE.Id != 3 {
		t.Errorf("next update pts %d count %d id %d, want 6, 1, 3", u.Pts_INT32, u.PtsCount, u.Message_MESSAGE.Id)
	}
}

func TestRunNoChange(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	res, err := Run(context.Background(), db, 7, func(*Box) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Pts != BasePts || len(res.Updates) != 0 {
		t.Errorf("result %+v", res)
	}
	if s.count("journal") != 0 || s.count("update_state") != 0 {
		t.Errorf("a transaction without changes wrote the counters: %v", s.statements())
	}
}

func TestRunBusinessError(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	errBusiness := errors.New("MSG_ID_INVALID")
	_, err := Run(context.Background(), db, 7, func(b *Box) error {
		if err := sendMessage(b); err != nil {
			return err
		}
		return errBusiness
	})
	if err != errBusiness {
		t.Fatalf("error %v, want the business error as is", err)
	}
	if s.count("begin 2") != 1 || s.count("commit") != 0 || s.count("rollback") != 1 {
		t.Errorf("statements %v: want one attempt, rolled back", s.statements())
	}
	if _, ok := s.state(7); ok || len(s.otherWrites()) != 0 {
		t.Errorf("something was committed")
	}
}

func TestRunRetries(t *testing.T) {
	for _, c := range []struct {
		name     string
		err      error
		attempts int
	}{
		{"deadlock", mysqlErr(1213), 2},
		{"lock wait timeout", mysqlErr(1205), 2},
		{"duplicate key", mysqlErr(1062), 1},
		{"other error", errors.New("broken"), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newFakeServer()
			db := s.open(t)
			s.failNext("write_other", injection{err: c.err})
			attempt := 0
			res, err := Run(context.Background(), db, 7, func(b *Box) error {
				attempt++
				b.Emit(Event{Topic: "Sync-T", Recipient: 2, Method: fmt.Sprint("attempt ", attempt), Data: []byte("{}")})
				b.InvalidateCache(fmt.Sprint("attempt ", attempt))
				return sendMessage(b)
			})
			if got := s.count("begin 2"); got != c.attempts {
				t.Errorf("%d attempts, want %d", got, c.attempts)
			}
			if c.attempts == 1 {
				if !errors.Is(err, c.err) {
					t.Errorf("error %v, want %v", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Only the second attempt is in the result.
			if len(res.Updates) != 1 || res.Updates[0].Pts_INT32 != 2 || res.Updates[0].Message_MESSAGE.Id != 1 {
				t.Errorf("updates %v", res.Updates)
			}
			if res.Emitted != 2 {
				t.Errorf("%d events emitted, want 2", res.Emitted)
			}
			if ev := s.events(); len(ev) != 2 || ev[0].method != "attempt 2" || ev[1].method != "send" {
				t.Errorf("events %+v", ev)
			}
			if !slices.Equal(res.CacheKeys, []string{"attempt 2", "dialog_1"}) {
				t.Errorf("cache keys %v", res.CacheKeys)
			}
			if rows := s.rows(7); len(rows) != 1 || rows[0].pts != 2 {
				t.Errorf("journal %+v", rows)
			}
		})
	}
}

func TestRunGivesUp(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	for range MaxAttempts {
		s.failNext("write_other", injection{err: mysqlErr(1205)})
	}
	_, err := Run(context.Background(), db, 7, sendMessage)
	if n, _ := mysqlErrorNumber(err); n != 1205 {
		t.Fatalf("error %v, want 1205", err)
	}
	if got := s.count("begin 2"); got != MaxAttempts {
		t.Errorf("%d attempts, want %d", got, MaxAttempts)
	}
}

// fn ignores a failed statement and goes on writing: nothing more reaches
// the server, nothing is committed, Run returns the statement's error.
func TestRunIgnoredError(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	s.failNext("write_other", injection{err: mysqlErr(1062)})
	_, err := Run(context.Background(), db, 7, func(b *Box) error {
		_, _ = b.Tx().Exec("INSERT INTO messages (id) VALUES (1)")
		_, _ = b.Tx().Exec("INSERT INTO hash_tags (id) VALUES (1)")
		var v int
		_ = b.Tx().QueryRow(&v, "SELECT v FROM t")
		b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
		return nil
	})
	if n, _ := mysqlErrorNumber(err); n != 1062 {
		t.Fatalf("error %v, want 1062", err)
	}
	if s.count("write_other") != 1 || s.count("select_other") != 0 {
		t.Errorf("statements after the failed one reached the server: %v", s.statements())
	}
	if s.count("commit") != 0 || s.count("rollback") != 1 {
		t.Errorf("statements %v: want a rollback and no commit", s.statements())
	}
	if _, ok := s.state(7); ok || len(s.otherWrites()) != 0 {
		t.Error("something was committed")
	}
}

// After a deadlock the server has rolled back and runs every statement in
// autocommit mode: statements after it must not reach the server.
func TestRunIgnoredDeadlock(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	s.failNext("write_other", injection{err: mysqlErr(1213)})
	attempt := 0
	res, err := Run(context.Background(), db, 7, func(b *Box) error {
		attempt++
		_, _ = b.Tx().Exec("INSERT INTO messages (id) VALUES (?)", attempt)
		_, _ = b.Tx().Exec(fmt.Sprintf("INSERT INTO marker (attempt) VALUES (%d)", attempt))
		b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Pts != BasePts+1 {
		t.Errorf("pts %d, want %d", res.State.Pts, BasePts+1)
	}
	if got := s.otherWrites(); !slices.Equal(got, []string{"INSERT INTO messages (id) VALUES (?)", "INSERT INTO marker (attempt) VALUES (2)"}) {
		t.Errorf("committed writes %v: only the second attempt's", got)
	}
	if rows := s.rows(7); len(rows) != 1 {
		t.Errorf("journal %+v", rows)
	}
}

// A read that finds no row is a result: the transaction goes on.
func TestRunNoRowsIsNotAnError(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	_, err := Run(context.Background(), db, 7, func(b *Box) error {
		var v struct {
			V int `db:"v"`
		}
		if err := b.Tx().QueryRow(&v, "SELECT v FROM t WHERE id = ?", 1); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("read of a missing row: %v", err)
		}
		return sendMessage(b)
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.count("commit") != 1 {
		t.Errorf("statements %v", s.statements())
	}
}

func TestRunInsideRun(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	_, err := Run(context.Background(), db, 7, func(b *Box) error {
		if _, err := Run(b.Context(), db, 8, sendMessage); !errors.Is(err, ErrInsideRun) {
			t.Errorf("nested Run: %v", err)
		}
		if _, err := Read(b.Context(), db, 7); !errors.Is(err, ErrInsideRun) {
			t.Errorf("Read inside Run: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.state(8); ok {
		t.Error("the nested Run wrote")
	}
}

func TestRunMisuse(t *testing.T) {
	for _, c := range []struct {
		name string
		fn   func(b *Box)
	}{
		{"zero pts_count", func(b *Box) { b.AddPtsUpdate(newMessage(1), 0) }},
		{"nil update", func(b *Box) { b.AddPtsUpdate(nil, 1) }},
		{"channel update", func(b *Box) {
			b.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{}).To_Update(), 1)
		}},
		{"not a pts update", func(b *Box) {
			b.AddPtsUpdate(mtproto.MakeTLUpdateUserTyping(&mtproto.Update{}).To_Update(), 1)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newFakeServer()
			db := s.open(t)
			_, err := Run(context.Background(), db, 7, func(b *Box) error {
				b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
				c.fn(b)
				return nil
			})
			if err == nil {
				t.Fatal("no error")
			}
			if s.count("commit") != 0 || s.count("begin 2") != 1 {
				t.Errorf("statements %v: want one attempt, no commit", s.statements())
			}
		})
	}
}

func TestRunInvalidUser(t *testing.T) {
	s := newFakeServer()
	if _, err := Run(context.Background(), s.open(t), 0, sendMessage); err == nil {
		t.Fatal("no error for user 0")
	}
}

// Another writer changed the counters under the lock: the conditional
// UPDATE finds nothing to change, and nothing is committed.
func TestRunStateChanged(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	if _, err := Run(context.Background(), db, 7, sendMessage); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), db, 7, func(b *Box) error {
		s.mu.Lock()
		st := s.states[7]
		st.Pts += 10
		s.states[7] = st
		s.mu.Unlock()
		return sendMessage(b)
	})
	if !errors.Is(err, errStateChanged) {
		t.Fatalf("error %v, want %v", err, errStateChanged)
	}
	if rows := s.rows(7); len(rows) != 1 {
		t.Errorf("journal %+v: the second transaction's row was committed", rows)
	}
}

func TestRunCommitOutcome(t *testing.T) {
	broken := errors.New("connection reset")
	t.Run("applied", func(t *testing.T) {
		s := newFakeServer()
		db := s.open(t)
		s.failNext("commit", injection{err: broken, afterApply: true, breakConn: true})
		res, err := Run(context.Background(), db, 7, sendMessage)
		if err != nil {
			t.Fatal(err)
		}
		if res.Emitted != 1 || res.State.Pts != BasePts+1 {
			t.Errorf("result %+v", res)
		}
		if s.count("verify_lock") != 1 || s.count("verify_row") != 1 || s.count("lock") != 1 {
			t.Errorf("statements %v: want one attempt and a check", s.statements())
		}
	})
	t.Run("not applied", func(t *testing.T) {
		s := newFakeServer()
		db := s.open(t)
		if _, err := Run(context.Background(), db, 7, sendMessage); err != nil {
			t.Fatal(err)
		}
		s.failNext("commit", injection{err: broken, breakConn: true})
		res, err := Run(context.Background(), db, 7, sendMessage)
		if err != nil {
			t.Fatal(err)
		}
		if res.State.Pts != BasePts+2 {
			t.Errorf("pts %d, want %d", res.State.Pts, BasePts+2)
		}
		if rows := s.rows(7); len(rows) != 2 || rows[1].pts != BasePts+2 {
			t.Errorf("journal %+v: want one set of rows per operation", rows)
		}
		if s.count("verify_lock") != 1 || s.count("lock") != 3 {
			t.Errorf("statements %v: want a check and a second attempt", s.statements())
		}
	})
	t.Run("unknown", func(t *testing.T) {
		s := newFakeServer()
		db := s.open(t)
		s.failNext("commit", injection{err: broken, breakConn: true})
		s.failNext("verify_lock", injection{err: mysqlErr(1205)})
		_, err := Run(context.Background(), db, 7, sendMessage)
		if !errors.Is(err, ErrCommitUnknown) {
			t.Fatalf("error %v, want %v", err, ErrCommitUnknown)
		}
		if s.count("lock") != 1 {
			t.Errorf("an unknown commit was tried again: %v", s.statements())
		}
	})
	t.Run("no journal", func(t *testing.T) {
		s := newFakeServer()
		db := s.open(t)
		s.failNext("commit", injection{err: broken, breakConn: true})
		_, err := Run(context.Background(), db, 7, func(b *Box) error {
			_, err := b.Tx().Exec("UPDATE dialogs SET pinned = 1 WHERE id = ?", 1)
			return err
		})
		if !errors.Is(err, ErrCommitUnknown) {
			t.Fatalf("error %v, want %v", err, ErrCommitUnknown)
		}
		if s.count("verify_lock") != 0 {
			t.Errorf("checked a commit without journal rows: %v", s.statements())
		}
	})
}

// The deadline ends the attempt; nothing is committed or retried.
func TestRunDeadline(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, db, 7, func(b *Box) error {
		<-b.Context().Done()
		return sendMessage(b)
	})
	if err == nil {
		t.Fatal("no error")
	}
	if s.count("begin 2") != 1 || s.count("commit") != 0 {
		t.Errorf("statements %v", s.statements())
	}
	if _, ok := s.state(7); ok {
		t.Error("committed after the deadline")
	}
}

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		err   error
		code  string
		retry bool
	}{
		{mysqlErr(1213), "1213", true},
		{fmt.Errorf("wrapped: %w", mysqlErr(1205)), "1205", true},
		{errNotApplied, "commit", true},
		{mysqlErr(1062), "1062", false},
		{ErrCommitUnknown, "commit_unknown", false},
		{errStateChanged, "state_changed", false},
		{context.DeadlineExceeded, "context", false},
		{driver.ErrBadConn, "other", false},
	} {
		code, retry := classify(c.err)
		if code != c.code || retry != c.retry {
			t.Errorf("classify(%v) = %s, %v; want %s, %v", c.err, code, retry, c.code, c.retry)
		}
	}
}

// A caller's own deadline is not retryable; Run's budget running out while
// the caller waits is: nothing was written.
func TestRunBudgetIsRetryable(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, db, 7, func(b *Box) error {
		<-b.Context().Done()
		return sendMessage(b)
	})
	if Retryable(err) || errors.Is(err, ErrBudget) {
		t.Errorf("caller's deadline: %v, retryable", err)
	}
}

func TestRetryable(t *testing.T) {
	for _, c := range []struct {
		err   error
		retry bool
	}{
		{nil, false},
		{mysqlErr(1213), true},
		{fmt.Errorf("wrapped: %w", mysqlErr(1205)), true},
		{errNotApplied, true},
		{fmt.Errorf("%w: %w", ErrBudget, context.DeadlineExceeded), true},
		{driver.ErrBadConn, true},
		{mysql.ErrInvalidConn, true},
		{ErrCommitUnknown, false},
		{mysqlErr(1062), false},
		{context.DeadlineExceeded, false},
		{errors.New("MSG_ID_INVALID"), false},
	} {
		if got := Retryable(c.err); got != c.retry {
			t.Errorf("Retryable(%v) = %v, want %v", c.err, got, c.retry)
		}
	}
}

func TestTimeLimits(t *testing.T) {
	// Every attempt can wait the whole lock wait timeout within the budget.
	if Budget < MaxAttempts*LockWaitTimeout {
		t.Errorf("budget %v < %d attempts × %v", Budget, MaxAttempts, LockWaitTimeout)
	}
	if MaxRunTime != Budget+CommitTimeout+VerifyTimeout {
		t.Errorf("MaxRunTime %v", MaxRunTime)
	}
}

func TestRead(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	st, err := Read(context.Background(), db, 7)
	if err != nil || st != (State{Pts: BasePts}) {
		t.Fatalf("state of a new user: %+v, %v", st, err)
	}
	s.states[7] = State{Pts: 42, LastMessageBoxId: 9, Date: 100}
	if st, err = Read(context.Background(), db, 7); err != nil || st != s.states[7] {
		t.Fatalf("state %+v, %v; want %+v", st, err, s.states[7])
	}
}

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		name     string
		settings []driver.Value
		bad      string
	}{
		{"ok", []driver.Value{int64(2), int64(1), int64(1), "ROW", int64(1), int64(64 << 20)}, ""},
		{"ok without binlog", []driver.Value{int64(1), int64(1), int64(0), "STATEMENT", int64(0), int64(16 << 20)}, ""},
		{"lock wait", []driver.Value{int64(50), int64(1), int64(1), "ROW", int64(1), int64(64 << 20)}, "innodb_lock_wait_timeout"},
		{"flush", []driver.Value{int64(2), int64(2), int64(1), "ROW", int64(1), int64(64 << 20)}, "innodb_flush_log_at_trx_commit"},
		{"format", []driver.Value{int64(2), int64(1), int64(1), "MIXED", int64(1), int64(64 << 20)}, "binlog_format"},
		{"sync", []driver.Value{int64(2), int64(1), int64(1), "ROW", int64(0), int64(64 << 20)}, "sync_binlog"},
		{"packet", []driver.Value{int64(2), int64(1), int64(1), "ROW", int64(1), int64(4 << 20)}, "max_allowed_packet"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newFakeServer()
			s.settings = c.settings
			err := Check(context.Background(), s.open(t))
			switch {
			case c.bad == "" && err != nil:
				t.Fatal(err)
			case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
				t.Fatalf("error %v, want one naming %s", err, c.bad)
			}
		})
	}
}
