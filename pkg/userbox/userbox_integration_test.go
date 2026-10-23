//go:build integration

package userbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

// The stores of this package's integration tests: MySQL with the whole
// schema, MySQL with the schema before migrate-20261022.sql, and Redis.
var (
	itDB    *testinfra.MySQL
	itOldDB *testinfra.MySQL
	itRedis *testinfra.Redis
)

const migration = "migrate-20261022.sql"

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 3)
	)
	wg.Add(3)
	go func() { defer wg.Done(); itDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() {
		defer wg.Done()
		files, err := testinfra.SchemaFilesBefore(migration)
		if err != nil {
			errs[1] = err
			return
		}
		itOldDB, errs[1] = testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	}()
	go func() { defer wg.Done(); itRedis, errs[2] = testinfra.StartRedis(ctx) }()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	_ = itDB.Terminate(ctx)
	_ = itOldDB.Terminate(ctx)
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

// itUserSeq gives each test its own users, so the tests run in any order
// and again (-count=N) on the same database.
var itUserSeq atomic.Int64

func itUser() int64 { return 10_000_000 + itUserSeq.Add(1) }

// boxDSN is dsn with the session lock wait timeout Run requires.
func boxDSN(dsn string) string {
	return dsn + fmt.Sprintf("&innodb_lock_wait_timeout=%d", int(LockWaitTimeout.Seconds()))
}

func openPool(t testing.TB, dsn string, maxConns int) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// pools runs f on a pool like the services' and on a pool of one
// connection. On the latter a read through the pool inside a transaction
// would wait for a connection until the deadline, so every scenario passing
// on it shows that Run and fn use the transaction's connection only.
func pools(t *testing.T, f func(t *testing.T, db *sql.DB)) {
	t.Run("pool", func(t *testing.T) { f(t, openPool(t, boxDSN(itDB.DSN()), 32)) })
	t.Run("one connection", func(t *testing.T) { f(t, openPool(t, boxDSN(itDB.DSN()), 1)) })
}

// insertMessage writes a messages row for box id id of the box's user.
func insertMessage(b *Box, id int32) error {
	_, err := b.Tx().Exec(
		"INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, date2) VALUES (?, ?, 0, 0, ?, ?, 2, 0, ?)",
		b.UserId(), id, int64(id), b.UserId(), b.Date())
	return err
}

// send writes a message and its updateNewMessage.
func send(b *Box) error {
	id := b.NextMessageId()
	if err := insertMessage(b, id); err != nil {
		return err
	}
	b.AddPtsUpdate(newMessage(id), 1)
	return nil
}

func mustRun(t testing.TB, db *sql.DB, userId int64, fn func(b *Box) error) *Result {
	t.Helper()
	res, err := Run(context.Background(), db, userId, fn)
	if err != nil {
		t.Fatalf("Run for user %d: %v", userId, err)
	}
	return res
}

type journalEntry struct {
	id       int64
	pts      int32
	ptsCount int32
}

// journalOf returns the user's journal rows in id order.
func journalOf(t testing.TB, db *sql.DB, userId int64) []journalEntry {
	t.Helper()
	rows, err := db.Query("SELECT id, pts, pts_count FROM user_pts_updates WHERE user_id = ? ORDER BY id", userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []journalEntry
	for rows.Next() {
		var e journalEntry
		if err := rows.Scan(&e.id, &e.pts, &e.ptsCount); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// queryInts returns the first column of a query's rows.
func queryInts(t testing.TB, db *sql.DB, query string, args ...any) []int64 {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func boxIdsOf(t testing.TB, db *sql.DB, userId int64) []int32 {
	t.Helper()
	rows, err := db.Query("SELECT user_message_box_id FROM messages WHERE user_id = ? ORDER BY user_message_box_id", userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustRead(t testing.TB, db *sql.DB, userId int64) State {
	t.Helper()
	st, err := Read(context.Background(), db, userId)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func mustCheckJournal(t testing.TB, db *sql.DB, userId int64) JournalReport {
	t.Helper()
	rep, err := CheckJournal(context.Background(), db, userId, BasePts)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("journal of user %d: gaps %v, overlaps %v", userId, rep.Gaps, rep.Overlaps)
	}
	return rep
}

func seq(from, to int32) []int32 {
	var out []int32
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

func TestSequentialOperations(t *testing.T) {
	pools(t, func(t *testing.T, db *sql.DB) {
		u := itUser()
		if st := mustRead(t, db, u); st != (State{Pts: BasePts}) {
			t.Fatalf("state of a new user: %+v", st)
		}
		for range 10 {
			mustRun(t, db, u, send)
		}
		res := mustRun(t, db, u, func(b *Box) error {
			b.AddPtsUpdate(deleteMessages(1, 2, 3), 3)
			return nil
		})
		if want := int32(BasePts + 10 + 3); res.State.Pts != want {
			t.Errorf("pts %d, want %d", res.State.Pts, want)
		}
		st := mustRead(t, db, u)
		if st.Pts != res.State.Pts || st.LastMessageBoxId != 10 {
			t.Errorf("committed state %+v, want pts %d, box id 10", st, res.State.Pts)
		}
		rep := mustCheckJournal(t, db, u)
		if rep.Rows != 11 {
			t.Errorf("%d journal rows, want 11", rep.Rows)
		}
		if got := boxIdsOf(t, db, u); !slices.Equal(got, seq(1, 10)) {
			t.Errorf("box ids %v", got)
		}
	})
}

// CheckJournal reports pts no row covers and pts covered twice.
func TestCheckJournalFindsGaps(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 2)
	u := itUser()
	if _, err := db.Exec("INSERT INTO user_state (user_id, pts) VALUES (?, 7)", u); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]int{{2, 1}, {4, 1}, {5, 2}} { // (1,2] (3,4] (3,5]
		if _, err := db.Exec("INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES (?, ?, ?, 1, '{}', 0)", u, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := CheckJournal(context.Background(), db, u, BasePts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || rep.Rows != 3 ||
		!slices.Equal(rep.Gaps, []Range{{From: 2, To: 3}, {From: 5, To: 7}}) ||
		!slices.Equal(rep.Overlaps, []Range{{From: 3, To: 4}}) {
		t.Errorf("report %+v", rep)
	}
}

// An operation that fails after allocating leaves no trace; the next one
// gets the next pts and box id.
func TestRollbackLeavesNoGap(t *testing.T) {
	pools(t, func(t *testing.T, db *sql.DB) {
		u := itUser()
		mustRun(t, db, u, send)
		errBusiness := errors.New("MESSAGE_ID_INVALID")
		_, err := Run(context.Background(), db, u, func(b *Box) error {
			if err := send(b); err != nil {
				return err
			}
			return errBusiness
		})
		if !errors.Is(err, errBusiness) {
			t.Fatalf("error %v", err)
		}
		if st := mustRead(t, db, u); st.Pts != BasePts+1 || st.LastMessageBoxId != 1 {
			t.Errorf("state after rollback %+v", st)
		}
		res := mustRun(t, db, u, send)
		if res.State.Pts != BasePts+2 || res.State.LastMessageBoxId != 2 {
			t.Errorf("state %+v, want pts %d, box id 2", res.State, BasePts+2)
		}
		mustCheckJournal(t, db, u)
		if got := boxIdsOf(t, db, u); !slices.Equal(got, []int32{1, 2}) {
			t.Errorf("box ids %v", got)
		}
	})
}

// A journal row already on the next pts (UNIQUE (user_id, pts)) fails the
// operation; nothing of it is written.
func TestTakenPtsFails(t *testing.T) {
	pools(t, func(t *testing.T, db *sql.DB) {
		u := itUser()
		mustRun(t, db, u, send)
		if _, err := db.Exec("INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES (?, ?, 1, 1, '{}', 0)", u, BasePts+2); err != nil {
			t.Fatal(err)
		}
		_, err := Run(context.Background(), db, u, send)
		if n, _ := mysqlErrorNumber(err); n != 1062 {
			t.Fatalf("error %v, want a duplicate key", err)
		}
		if st := mustRead(t, db, u); st.Pts != BasePts+1 || st.LastMessageBoxId != 1 {
			t.Errorf("state %+v", st)
		}
		if got := boxIdsOf(t, db, u); !slices.Equal(got, []int32{1}) {
			t.Errorf("box ids %v", got)
		}
		if got := journalOf(t, db, u); len(got) != 2 {
			t.Errorf("journal %+v", got)
		}
	})
}

// fn ignores a duplicate key error and goes on writing: Run fails and
// nothing of the attempt is in the database.
func TestIgnoredErrorWritesNothing(t *testing.T) {
	pools(t, func(t *testing.T, db *sql.DB) {
		u := itUser()
		_, err := Run(context.Background(), db, u, func(b *Box) error {
			id := b.NextMessageId()
			_ = insertMessage(b, id)
			_ = insertMessage(b, id) // duplicate key
			_ = insertMessage(b, b.NextMessageId())
			b.AddPtsUpdate(newMessage(id), 1)
			return nil
		})
		if n, _ := mysqlErrorNumber(err); n != 1062 {
			t.Fatalf("error %v, want a duplicate key", err)
		}
		if got := boxIdsOf(t, db, u); len(got) != 0 {
			t.Errorf("messages written: %v", got)
		}
		if got := journalOf(t, db, u); len(got) != 0 {
			t.Errorf("journal written: %+v", got)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM user_state WHERE user_id = ?", u).Scan(&n); err != nil || n != 0 {
			t.Errorf("user_state row written: %d, %v", n, err)
		}
	})
}

// N concurrent operations on one user: pts from BasePts+1 without gaps or
// repeats, journal commit order = pts order, box ids unique and dense. A
// reader taking the state and the rows up to it never sees a gap.
func TestConcurrentOperations(t *testing.T) {
	const workers, perWorker = 32, 25
	pools(t, func(t *testing.T, db *sql.DB) {
		u := itUser()
		reader := openPool(t, boxDSN(itDB.DSN()), 2)

		stop := make(chan struct{})
		readerErr := make(chan error, 1)
		go func() {
			var last int32
			for reads := 0; ; reads++ {
				select {
				case <-stop:
					readerErr <- nil
					return
				default:
				}
				rep, err := CheckJournal(context.Background(), reader, u, BasePts)
				if err != nil {
					readerErr <- err
					return
				}
				if !rep.OK() {
					readerErr <- fmt.Errorf("read %d: gaps %v, overlaps %v", reads, rep.Gaps, rep.Overlaps)
					return
				}
				if rep.State.Pts < last {
					readerErr <- fmt.Errorf("read %d: pts went back from %d to %d", reads, last, rep.State.Pts)
					return
				}
				last = rep.State.Pts
			}
		}()

		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			pts  []int32
			errs []error
		)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range perWorker {
					res, err := Run(context.Background(), db, u, send)
					mu.Lock()
					if err != nil {
						errs = append(errs, err)
					} else {
						pts = append(pts, res.Updates[0].Pts_INT32)
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		close(stop)
		if err := <-readerErr; err != nil {
			t.Error(err)
		}
		if len(errs) > 0 {
			t.Fatalf("%d operations failed, first: %v", len(errs), errs[0])
		}

		const total = workers * perWorker
		slices.Sort(pts)
		if !slices.Equal(pts, seq(BasePts+1, BasePts+total)) {
			t.Errorf("pts returned are not %d..%d without gaps or repeats", BasePts+1, BasePts+total)
		}
		journal := journalOf(t, db, u)
		if len(journal) != total {
			t.Fatalf("%d journal rows, want %d", len(journal), total)
		}
		for i, e := range journal {
			if e.pts != int32(BasePts+1+i) || e.ptsCount != 1 {
				t.Fatalf("journal row %d (id order): pts %d count %d, want pts %d", i, e.pts, e.ptsCount, BasePts+1+i)
			}
		}
		if got := boxIdsOf(t, db, u); !slices.Equal(got, seq(1, total)) {
			t.Errorf("box ids are not 1..%d", total)
		}
		if st := mustRead(t, db, u); st.Pts != BasePts+total || st.LastMessageBoxId != total {
			t.Errorf("state %+v", st)
		}
	})
}

// deadlockRows is a table of per-user rows for the deadlock test, standing
// in for a user's dialogs.
const deadlockRows = "userbox_it_rows"

func createDeadlockRows(t *testing.T, db *sql.DB, u int64) {
	t.Helper()
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS " + deadlockRows + " (user_id BIGINT NOT NULL, k INT NOT NULL, v INT NOT NULL DEFAULT 0, PRIMARY KEY (user_id, k)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	for k := range 12 {
		if _, err := db.Exec("INSERT INTO "+deadlockRows+" (user_id, k) VALUES (?, ?)", u, k); err != nil {
			t.Fatal(err)
		}
	}
}

// waitLockWait waits until a transaction waits for a lock.
func waitLockWait(t *testing.T, root *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := root.QueryRow("SELECT COUNT(*) FROM information_schema.INNODB_TRX WHERE trx_state = 'LOCK WAIT'").Scan(&n); err != nil {
			t.Error(err)
			return
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("no transaction started waiting for a lock")
}

// A real deadlock: another transaction holds row B and waits for row A,
// which Run holds; Run then asks for B. The other transaction changed more
// rows, so InnoDB rolls Run back (1213); Run is tried again and succeeds,
// with the journal and pts of the second attempt only. In the second case
// fn ignores the deadlock error and goes on writing: those writes must not
// reach the server, which runs in autocommit mode after a deadlock.
func TestDeadlockRetried(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignored %v", ignore), func(t *testing.T) {
			db := openPool(t, boxDSN(itDB.DSN()), 8)
			root := openPool(t, itDB.RootDSN(), 2)
			u := itUser()
			createDeadlockRows(t, db, u)
			upd := "UPDATE " + deadlockRows + " SET v = v + 1 WHERE user_id = ? AND k = ?"

			other, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = other.Rollback() }()
			if _, err = other.Exec("UPDATE "+deadlockRows+" SET v = v + 1 WHERE user_id = ? AND k >= 2", u); err != nil {
				t.Fatal(err)
			}
			if _, err = other.Exec(upd, u, 1); err != nil { // B
				t.Fatal(err)
			}

			aLocked := make(chan struct{})
			otherDone := make(chan error, 1)
			go func() {
				<-aLocked
				_, err := other.Exec(upd, u, 0) // A: waits for Run
				if err == nil {
					err = other.Commit()
				}
				otherDone <- err
			}()

			retriesBefore := counter(t, "userbox_tx_retries_total", "1213")
			attempts := 0
			res, err := Run(context.Background(), db, u, func(b *Box) error {
				attempts++
				if _, err := b.Tx().Exec(upd, u, 0); err != nil { // A
					return err
				}
				if attempts == 1 {
					close(aLocked)
					waitLockWait(t, root)
				}
				if _, err := b.Tx().Exec(upd, u, 1); err != nil { // B: deadlock on the first attempt
					if !ignore {
						return err
					}
				}
				_, _ = b.Tx().Exec("INSERT INTO "+deadlockRows+" (user_id, k) VALUES (?, ?)", u, 100+attempts)
				b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := <-otherDone; err != nil {
				t.Fatalf("the other transaction: %v", err)
			}
			if attempts != 2 {
				t.Errorf("%d attempts, want 2", attempts)
			}
			if got := counter(t, "userbox_tx_retries_total", "1213") - retriesBefore; got != 1 {
				t.Errorf("userbox_tx_retries_total{code=1213} grew by %v, want 1", got)
			}
			if res.State.Pts != BasePts+1 {
				t.Errorf("pts %d, want %d", res.State.Pts, BasePts+1)
			}
			if got := journalOf(t, db, u); len(got) != 1 || got[0].pts != BasePts+1 {
				t.Errorf("journal %+v", got)
			}
			markers := queryInts(t, db, "SELECT k FROM "+deadlockRows+" WHERE user_id = ? AND k >= 100 ORDER BY k", u)
			if !slices.Equal(markers, []int64{102}) {
				t.Errorf("markers %v: want the second attempt's only", markers)
			}
			var a, bv int
			if err := db.QueryRow("SELECT (SELECT v FROM "+deadlockRows+" WHERE user_id = ? AND k = 0), (SELECT v FROM "+deadlockRows+" WHERE user_id = ? AND k = 1)", u, u).Scan(&a, &bv); err != nil || a != 2 || bv != 2 {
				t.Errorf("rows A, B: %d, %d, %v; want 2, 2", a, bv, err)
			}
		})
	}
}

// Another transaction holds the user's row: Run gives up within its budget,
// and its server-side transaction is gone a lock wait timeout later.
func TestLockHeldLongerThanBudget(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 8)
	root := openPool(t, itDB.RootDSN(), 2)
	u := itUser()
	mustRun(t, db, u, send)

	holder, err := root.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	var holderThread int64
	if err = holder.QueryRow("SELECT CONNECTION_ID()").Scan(&holderThread); err != nil {
		t.Fatal(err)
	}
	if _, err = holder.Exec("SELECT pts FROM user_state WHERE user_id = ? FOR UPDATE", u); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = Run(context.Background(), db, u, send)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Run succeeded while the row was locked")
	}
	if elapsed > Budget+time.Second {
		t.Errorf("Run took %v, budget %v", elapsed, Budget)
	}
	if !errors.Is(err, ErrBudget) || !Retryable(err) {
		t.Errorf("Run gave up with %v: want ErrBudget, retryable", err)
	}
	t.Logf("Run gave up after %v: %v", elapsed, err)

	time.Sleep(LockWaitTimeout + time.Second)
	var n int
	if err = root.QueryRow("SELECT COUNT(*) FROM information_schema.INNODB_TRX WHERE trx_mysql_thread_id <> ?", holderThread).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d transactions besides the holder still open", n)
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if res := mustRun(t, db, u, send); res.State.Pts != BasePts+2 {
		t.Errorf("pts after the lock is released: %d", res.State.Pts)
	}
}

// COMMIT whose reply is lost: Run finds out on another connection whether it
// was applied, through a proxy that breaks the connection at COMMIT.
func TestCommitOutcomeUnknown(t *testing.T) {
	cfg, err := mysql.ParseDSN(boxDSN(itDB.DSN()))
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := testinfra.StartMySQLProxy(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	cfg.Addr = proxy.Addr()
	db := openPool(t, cfg.FormatDSN(), 8)

	for _, c := range []struct {
		name     string
		fault    testinfra.CommitFault
		outcome  string
		attempts int
	}{
		{"applied", testinfra.CommitReplyLost, "applied", 1},
		{"not applied", testinfra.CommitNotSent, "not_applied", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := itUser()
			mustRun(t, db, u, send)
			before := counter(t, "userbox_commit_unknown_total", c.outcome)
			fired := proxy.Fired()
			proxy.FailNextCommit(c.fault)
			attempts := 0
			res, err := Run(context.Background(), db, u, func(b *Box) error {
				attempts++
				b.Emit(Event{Recipient: 2, Method: fmt.Sprint("attempt ", attempts)})
				return send(b)
			})
			if err != nil {
				t.Fatal(err)
			}
			if proxy.Fired() != fired+1 {
				t.Fatal("the proxy did not break the COMMIT")
			}
			if attempts != c.attempts {
				t.Errorf("%d attempts, want %d", attempts, c.attempts)
			}
			if len(res.Events) != 1 || res.Events[0].Method != fmt.Sprint("attempt ", c.attempts) {
				t.Errorf("events %+v", res.Events)
			}
			if got := counter(t, "userbox_commit_unknown_total", c.outcome) - before; got != 1 {
				t.Errorf("userbox_commit_unknown_total{outcome=%s} grew by %v, want 1", c.outcome, got)
			}
			if st := mustRead(t, db, u); st.Pts != BasePts+2 || res.State.Pts != st.Pts {
				t.Errorf("state %+v, result %+v", st, res.State)
			}
			if got := journalOf(t, db, u); len(got) != 2 {
				t.Errorf("journal %+v: want one row per operation", got)
			}
			if got := boxIdsOf(t, db, u); !slices.Equal(got, []int32{1, 2}) {
				t.Errorf("box ids %v", got)
			}
		})
	}
}

// The one-connection pool catches a read through the pool inside fn: it
// waits for the connection the transaction holds until the deadline.
func TestPoolReadInsideRunIsCaught(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Run(ctx, db, itUser(), func(b *Box) error {
		var one int
		return db.QueryRowContext(b.Context(), "SELECT 1").Scan(&one)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v, want the deadline", err)
	}
}

// The isolation level is the primitive's: READ COMMITTED even on a pool left
// at the server's REPEATABLE READ. @@transaction_isolation shows the
// session's default, not the running transaction's level, which InnoDB
// reports in INNODB_TRX.
func TestIsolationLevel(t *testing.T) {
	db := openPool(t, itDB.DSN(), 2)
	root := openPool(t, itDB.RootDSN(), 1)
	var level string
	if err := db.QueryRow("SELECT @@transaction_isolation").Scan(&level); err != nil || level != "REPEATABLE-READ" {
		t.Fatalf("pool isolation %q, %v", level, err)
	}
	mustRun(t, db, itUser(), func(b *Box) error {
		var thread int64
		if err := b.Tx().QueryRow(&thread, "SELECT CONNECTION_ID()"); err != nil {
			return err
		}
		return root.QueryRow("SELECT trx_isolation_level FROM information_schema.INNODB_TRX WHERE trx_mysql_thread_id = ?", thread).Scan(&level)
	})
	if level != "READ COMMITTED" {
		t.Errorf("isolation of the box transaction: %s", level)
	}
}

func TestCheckServerSettings(t *testing.T) {
	if err := Check(context.Background(), openPool(t, boxDSN(itDB.DSN()), 1)); err != nil {
		t.Errorf("settings of the production MySQL: %v", err)
	}
	err := Check(context.Background(), openPool(t, itDB.DSN()+"&innodb_lock_wait_timeout=50", 1))
	if err == nil || !strings.Contains(err.Error(), "innodb_lock_wait_timeout") {
		t.Errorf("lock wait timeout of 50 s: %v", err)
	}
}

// The sequences live in MySQL: emptying Redis changes nothing.
func TestRedisFlushKeepsSequences(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 4)
	u := itUser()
	for range 5 {
		mustRun(t, db, u, send)
	}
	code, _, err := itRedis.Container.Exec(context.Background(),
		[]string{"redis-cli", "--no-auth-warning", "-a", itRedis.Password, "FLUSHALL"}, tcexec.Multiplexed())
	if err != nil || code != 0 {
		t.Fatalf("FLUSHALL: exit %d, %v", code, err)
	}
	for range 5 {
		mustRun(t, db, u, send)
	}
	if st := mustRead(t, db, u); st.Pts != BasePts+10 || st.LastMessageBoxId != 10 {
		t.Errorf("state %+v", st)
	}
	mustCheckJournal(t, db, u)
	if got := boxIdsOf(t, db, u); !slices.Equal(got, seq(1, 10)) {
		t.Errorf("box ids %v", got)
	}
}

// stateRows returns user_state as text, row by row, to compare snapshots.
func stateRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT user_id, pts, qts, seq, last_message_box_id, date2, updated_at FROM user_state ORDER BY user_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			u, date2           int64
			pts, qts, sq, last int32
			updated            time.Time
		)
		if err := rows.Scan(&u, &pts, &qts, &sq, &last, &date2, &updated); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprint(u, pts, qts, sq, last, date2, updated.UnixNano()))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func applyMigration(t *testing.T) {
	t.Helper()
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	if err = itOldDB.ApplySQLFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

func journalIndexUnique(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var nonUnique int
	if err := db.QueryRow("SELECT MIN(non_unique) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'user_pts_updates' AND index_name = 'user_id'").Scan(&nonUnique); err != nil {
		t.Fatal(err)
	}
	return nonUnique == 0
}

// The migration on a populated database: every user starts 1 000 000 above
// the largest pts and box id it has; duplicate journal rows go, the newest
// stays; the index becomes unique. Running it again changes no row, also
// after the new version has worked.
func TestMigration(t *testing.T) {
	ctx := context.Background()
	if err := itOldDB.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	db := openPool(t, boxDSN(itOldDB.DSN()), 4)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// 1001: journal with a duplicate pts and messages; 1002: only a user;
	// 1003: messages only; 1004: no users row, journal and messages.
	for i, u := range []int64{1001, 1002, 1003} {
		exec("INSERT INTO users (id, access_hash, phone, country_code) VALUES (?, 1, ?, 'US')", u, fmt.Sprint("1201555010", i))
	}
	journal := func(u int64, pts int32) {
		exec("INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES (?, ?, 1, 1, JSON_OBJECT('pts', ?), 0)", u, pts, pts)
	}
	for _, p := range []int32{1, 2, 3, 3, 4, 5} {
		journal(1001, p)
	}
	journal(1004, 7)
	message := func(u int64, id int32) {
		exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id) VALUES (?, ?, 0, 0, 0, ?, 2, 0)", u, id, u)
	}
	for _, id := range []int32{1, 2, 12} {
		message(1001, id)
	}
	message(1003, 4)
	message(1004, 9)
	var newestDup int64
	if err := db.QueryRow("SELECT MAX(id) FROM user_pts_updates WHERE user_id = 1001 AND pts = 3").Scan(&newestDup); err != nil {
		t.Fatal(err)
	}

	applyMigration(t)

	for _, c := range []struct {
		user     int64
		pts, box int32
	}{
		{1001, 5 + 1_000_000, 12 + 1_000_000},
		{1002, 1 + 1_000_000, 1_000_000},
		{1003, 1 + 1_000_000, 4 + 1_000_000},
		{1004, 7 + 1_000_000, 9 + 1_000_000},
	} {
		if st := mustRead(t, db, c.user); st.Pts != c.pts || st.LastMessageBoxId != c.box || st.Date == 0 {
			t.Errorf("user %d: state %+v, want pts %d, box id %d", c.user, st, c.pts, c.box)
		}
	}
	ids := queryInts(t, db, "SELECT id FROM user_pts_updates WHERE user_id = 1001 AND pts = 3")
	if !slices.Equal(ids, []int64{newestDup}) {
		t.Errorf("rows of the duplicate pts: %v, want the newest %d only", ids, newestDup)
	}
	if !journalIndexUnique(t, db) {
		t.Error("user_pts_updates (user_id, pts) is not unique")
	}

	// Again: nothing changes.
	before := stateRows(t, db)
	applyMigration(t)
	if after := stateRows(t, db); !slices.Equal(before, after) {
		t.Errorf("a second run changed user_state:\n%v\n%v", before, after)
	}

	// The new version works, then the migration runs again: still nothing.
	res := mustRun(t, db, 1001, send)
	if res.State.Pts != 5+1_000_000+1 || res.Updates[0].Message_MESSAGE.Id != 12+1_000_000+1 {
		t.Errorf("first operation after the migration: %+v", res.State)
	}
	before = stateRows(t, db)
	applyMigration(t)
	if after := stateRows(t, db); !slices.Equal(before, after) {
		t.Errorf("a run after new writes changed user_state:\n%v\n%v", before, after)
	}
}

// The migration on an empty database (a fresh install) adds no rows.
func TestMigrationEmptyDatabase(t *testing.T) {
	if err := itOldDB.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	db := openPool(t, itOldDB.DSN(), 2)
	applyMigration(t)
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM user_state").Scan(&n); err != nil || n != 0 {
		t.Errorf("user_state rows: %d, %v", n, err)
	}
	if !journalIndexUnique(t, db) {
		t.Error("user_pts_updates (user_id, pts) is not unique")
	}
}

// The migration's speed on a larger journal, to plan the maintenance window
// (docs/ops): the unique index is built over the whole table.
func TestMigrationSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	ctx := context.Background()
	if err := itOldDB.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	db := openPool(t, itOldDB.DSN(), 2)
	const users, perUser, batch = 200, 250, 1000
	data := `{"predicate_name": "updateNewMessage", "message_MESSAGE": {"predicate_name": "message", "id": 1, "message": "` + strings.Repeat("x", 200) + `"}}`
	var (
		args []any
		n    int
	)
	flush := func() {
		if n == 0 {
			return
		}
		q := "INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES " +
			strings.TrimSuffix(strings.Repeat("(?, ?, 1, 1, ?, 0), ", n), ", ")
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
		args, n = args[:0], 0
	}
	for u := 1; u <= users; u++ {
		for p := 1; p <= perUser; p++ {
			args = append(args, u, p, data)
			if n++; n == batch {
				flush()
			}
		}
	}
	flush()

	start := time.Now()
	applyMigration(t)
	elapsed := time.Since(start)
	rows := users * perUser
	t.Logf("migration of %d journal rows (%d B of JSON each): %v, %.0f rows/s", rows, len(data), elapsed, float64(rows)/elapsed.Seconds())
}

// BenchmarkRun is the cost of one box transaction (a message row and its
// journal row): one after another for one user, and in parallel for 100.
func BenchmarkRun(b *testing.B) {
	db := openPool(b, boxDSN(itDB.DSN()), 64)
	b.Run("one user", func(b *testing.B) {
		u := itUser()
		for b.Loop() {
			if _, err := Run(context.Background(), db, u, send); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("100 users", func(b *testing.B) {
		var users []int64
		for range 100 {
			users = append(users, itUser())
		}
		var next atomic.Int64
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				u := users[next.Add(1)%int64(len(users))]
				if _, err := Run(context.Background(), db, u, send); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})
}
