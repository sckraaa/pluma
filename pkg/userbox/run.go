package userbox

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	msqlx "github.com/teamgram/marmota/pkg/stores/sqlx"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Time limits of Run (ADR 0011, p. 2). The deadline is common to all attempts.
const (
	// LockWaitTimeout is the most a statement may wait for a row lock: the
	// session innodb_lock_wait_timeout of every pool that runs Run (a DSN
	// parameter, verified by Check). The server then ends the wait with 1205.
	LockWaitTimeout = 2 * time.Second
	// MaxAttempts of a transaction: deadlocks, lock wait timeouts and commits
	// found not applied are tried again.
	MaxAttempts = 3
	// Budget of all attempts with the pauses between them. It is a backstop:
	// a lock wait usually ends with 1205 after LockWaitTimeout.
	Budget = 6 * time.Second
	// CommitTimeout limits COMMIT, which the request's deadline does not cancel.
	CommitTimeout = 5 * time.Second
	// VerifyTimeout limits finding out, on another connection, whether a
	// COMMIT that failed was applied.
	VerifyTimeout = 5 * time.Second
	// MaxRunTime is the longest Run can take.
	MaxRunTime = Budget + CommitTimeout + VerifyTimeout

	retryPauseMin = 10 * time.Millisecond
	retryPauseMax = 50 * time.Millisecond
)

// MySQL errors Run retries.
const (
	errLockDeadlock    = 1213 // ER_LOCK_DEADLOCK: the transaction was rolled back
	errLockWaitTimeout = 1205 // ER_LOCK_WAIT_TIMEOUT
)

var (
	// ErrCommitUnknown means COMMIT failed and it could not be found out
	// whether it was applied. Not retried: the operation may have happened.
	ErrCommitUnknown = errors.New("userbox: commit outcome unknown")

	// ErrBudget means the attempts of Run took its whole Budget, e.g.
	// waiting for a user locked by a long transaction. Nothing was written:
	// the operation may be tried again later (Retryable).
	ErrBudget = errors.New("userbox: transaction budget exhausted")

	// errNotApplied: COMMIT failed and was found not applied; nothing was
	// written, the transaction is tried again.
	errNotApplied = errors.New("userbox: commit not applied")

	// errStateChanged: the counters changed under the transaction's lock,
	// which only a writer bypassing Run can do.
	errStateChanged = errors.New("userbox: user_state changed during the transaction")
)

const (
	// lockStateQuery creates the user's row with the base state or takes the
	// exclusive lock of the existing one, atomically. SELECT ... FOR UPDATE
	// first and INSERT when missing would race two first writes into a
	// deadlock (REPEATABLE READ, gap locks) or a duplicate key error (READ
	// COMMITTED); INSERT IGNORE takes a shared lock whose upgrade deadlocks.
	lockStateQuery   = "INSERT INTO user_state (user_id, date2) VALUES (?, ?) ON DUPLICATE KEY UPDATE user_id = user_id"
	lockedStateQuery = selectStateQuery + " FOR UPDATE"
	// updateStateQuery checks in the database that the counters are what
	// the transaction locked; it must change exactly one row.
	updateStateQuery = "UPDATE user_state SET pts = ?, last_message_box_id = ?, date2 = ? WHERE user_id = ? AND pts = ? AND last_message_box_id = ?"
	insertJournalRow = "(?, ?, ?, ?, ?, ?)"
	insertJournal    = "INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES "
)

// boxKey marks the context of a Box.
type boxKey struct{}

func insideRun(ctx context.Context) bool { return ctx.Value(boxKey{}) != nil }

// Run changes userId's box in one MySQL transaction (READ COMMITTED): it
// locks the user's user_state row, calls fn, writes the journal rows, the
// new counters and the events, and commits. See the package documentation
// for what fn may do.
//
// A deadlock, a lock wait timeout or a commit found not applied rolls the
// attempt back and runs it again with a fresh Box: the updates, events, ids
// and cache keys of a failed attempt are dropped. fn's own error rolls back
// and is returned as is, without a retry. On success the Result holds the
// committed state, the journaled updates and the cache keys to delete;
// deleting them is the caller's job, after Run returns.
func Run(ctx context.Context, db *sql.DB, userId int64, fn func(b *Box) error) (*Result, error) {
	if insideRun(ctx) {
		return nil, ErrInsideRun
	}
	if userId <= 0 {
		return nil, fmt.Errorf("userbox: invalid user id %d", userId)
	}
	start := time.Now()
	defer func() { metricTxSeconds.ObserveFloat(time.Since(start).Seconds()) }()

	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, Budget)
	defer cancel()
	// overBudget marks the error of an attempt the budget cut short while
	// the caller's context lives: nothing was written (a COMMIT that may
	// have been sent ends in ErrCommitUnknown or a commit check instead).
	overBudget := func(err error) error {
		if ctx.Err() != nil && parent.Err() == nil && !errors.Is(err, ErrCommitUnknown) {
			return fmt.Errorf("%w: %w", ErrBudget, err)
		}
		return err
	}
	for attempt := 1; ; attempt++ {
		res, fnFailed, err := runAttempt(ctx, db, userId, fn)
		if err == nil {
			return res, nil
		}
		if fnFailed {
			return nil, err
		}
		code, retry := classify(err)
		if !errors.Is(err, ErrCommitUnknown) { // logged with its pts range
			logRollback(ctx, userId, attempt, err)
		}
		if !retry || attempt >= MaxAttempts || ctx.Err() != nil {
			metricTxErrors.Inc(code)
			return nil, overBudget(err)
		}
		metricTxRetries.Inc(code)
		pause := retryPauseMin + rand.N(retryPauseMax-retryPauseMin) //nolint:gosec // retry jitter, not a secret
		select {
		case <-ctx.Done():
			metricTxErrors.Inc(code)
			return nil, overBudget(err)
		case <-time.After(pause):
		}
	}
}

// runAttempt is one attempt of Run. fnFailed reports an error of fn itself
// (a business error): it is returned as is and not retried.
func runAttempt(ctx context.Context, db *sql.DB, userId int64, fn func(b *Box) error) (res *Result, fnFailed bool, err error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	ended := false
	defer func() {
		if !ended {
			_ = tx.Rollback()
		}
	}()

	bctx := context.WithValue(ctx, boxKey{}, true)
	rec := newRecorder(bctx, sqlx.NewSessionFromTx(tx))
	b := &Box{
		ctx:    bctx,
		rec:    rec,
		tx:     msqlx.NewTx(bctx, rec),
		userId: userId,
		now:    time.Now().Unix(),
	}

	lockStart := time.Now()
	_, _ = rec.ExecCtx(ctx, lockStateQuery, userId, b.now)
	var row stateRow
	_ = rec.QueryRowCtx(ctx, &row, lockedStateQuery, userId)
	if err = rec.failed(); err != nil {
		return nil, false, err
	}
	metricLockWaitSeconds.ObserveFloat(time.Since(lockStart).Seconds())
	b.state = row.state()
	b.pts = b.state.Pts
	b.boxId = b.state.LastMessageBoxId

	fnErr := fn(b)
	// A failed statement wins over fn's result: fn may have ignored it.
	if err = rec.failed(); err != nil {
		return nil, false, err
	}
	if fnErr != nil {
		return nil, true, fnErr
	}
	if b.err != nil {
		return nil, false, b.err
	}
	if b.unfinished() {
		return nil, false, ErrEventNotFinished
	}

	if b.allocated() {
		if err = b.checkJournal(); err != nil {
			return nil, false, err
		}
		if err = writeState(ctx, b); err != nil {
			return nil, false, err
		}
	}
	// The events go last: the ids they take are held for the shortest
	// time before commit, which keeps a recipient's consumer from seeing a
	// later row before this one commits (ADR 0012, p. 2).
	if err = writeEvents(ctx, b); err != nil {
		return nil, false, err
	}

	ended = true
	sent, err := commit(ctx, tx)
	if err == nil {
		return b.result(), false, nil
	}
	if !sent {
		return nil, false, err
	}
	// The outcome of COMMIT is unknown.
	if len(b.journal) == 0 {
		metricCommitUnknown.Inc("unknown")
		logx.WithContext(ctx).Errorf("userbox: user %d: commit outcome unknown, no journal to check", userId)
		return nil, false, ErrCommitUnknown
	}
	applied, verr := verifyCommit(ctx, db, userId, b.pts, b.journal)
	switch {
	case verr != nil:
		metricCommitUnknown.Inc("unknown")
		logx.WithContext(ctx).Errorf("userbox: user %d: commit outcome unknown, pts (%d, %d]", userId, b.state.Pts, b.pts)
		return nil, false, ErrCommitUnknown
	case applied:
		metricCommitUnknown.Inc("applied")
		return b.result(), false, nil
	default:
		metricCommitUnknown.Inc("not_applied")
		return nil, false, errNotApplied
	}
}

// writeState writes the journal rows and the new counters of the box.
func writeState(ctx context.Context, b *Box) error {
	if len(b.journal) > 0 {
		args := make([]any, 0, len(b.journal)*6)
		for _, r := range b.journal {
			args = append(args, b.userId, r.pts, r.ptsCount, r.updateType, r.data, b.now)
		}
		query := insertJournal + strings.Repeat(insertJournalRow+", ", len(b.journal)-1) + insertJournalRow
		if _, err := b.rec.ExecCtx(ctx, query, args...); err != nil {
			return err
		}
	}
	res, err := b.rec.ExecCtx(ctx, updateStateQuery, b.pts, b.boxId, b.now, b.userId, b.state.Pts, b.state.LastMessageBoxId)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errStateChanged
	}
	return nil
}

// commit commits tx under CommitTimeout, which the deadline of ctx does not
// cut short: once sent, COMMIT is waited for. sent reports whether COMMIT may
// have reached the server; if it failed after that, its outcome is unknown.
//
// COMMIT is sent as a statement because (*sql.Tx).Commit takes no context
// and could wait on a dead connection without end; with a context the
// driver closes the connection when the limit passes. database/sql cannot
// end a *sql.Tx without the driver, so tx.Commit then sends a second COMMIT:
// one more round trip, which the server answers without work (no
// transaction is open), and the connection goes back to the pool. Avoiding
// it would take a session of our own over *sql.Conn instead of go-zero's
// over *sql.Tx.
func commit(ctx context.Context, tx *sql.Tx) (sent bool, err error) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), CommitTimeout)
	defer cancel()
	if _, err = tx.ExecContext(cctx, "COMMIT"); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrTxDone) {
			// database/sql ended the transaction when ctx expired, before
			// COMMIT was sent: it was rolled back.
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return false, err
		}
		return true, err
	}
	_ = tx.Commit()
	return true, nil
}

// verifyCommit finds out whether a COMMIT that failed was applied. It locks
// the user's row on another connection: while the old transaction is still
// finishing on the server, it holds that lock, and the read waits for its
// outcome. pts below the transaction's means not applied (pts never goes
// down); otherwise its journal rows must all be there as written, compared
// as JSON by the server.
func verifyCommit(ctx context.Context, db *sql.DB, userId int64, pts int32, journal []journalRow) (applied bool, err error) {
	vctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VerifyTimeout)
	defer cancel()
	tx, err := db.BeginTx(vctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var committed int32
	err = tx.QueryRowContext(vctx, "SELECT pts FROM user_state WHERE user_id = ? FOR UPDATE", userId).Scan(&committed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // the row was to be created by that transaction
	}
	if err != nil {
		return false, err
	}
	if committed < pts {
		return false, nil
	}
	for _, r := range journal {
		var n int
		err = tx.QueryRowContext(vctx,
			"SELECT COUNT(*) FROM user_pts_updates WHERE user_id = ? AND pts = ? AND pts_count = ? AND update_type = ? AND update_data = CAST(? AS JSON)",
			userId, r.pts, r.ptsCount, r.updateType, r.data).Scan(&n)
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, nil
		}
	}
	return true, nil
}

// classify names an error of an attempt for the metrics and tells whether
// the attempt may be tried again.
func classify(err error) (code string, retry bool) {
	if n, ok := mysqlErrorNumber(err); ok {
		return fmt.Sprint(n), n == errLockDeadlock || n == errLockWaitTimeout
	}
	switch {
	case errors.Is(err, errNotApplied):
		return "commit", true
	case errors.Is(err, ErrCommitUnknown):
		return "commit_unknown", false
	case errors.Is(err, errStateChanged):
		return "state_changed", false
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "context", false
	}
	return "other", false
}

// Retryable tells whether an error of Run means that nothing was written
// and the whole operation may be tried again later: a deadlock or a lock
// wait timeout that outlasted Run's own attempts or its budget, a COMMIT
// found not applied, or a connection lost before COMMIT. ErrCommitUnknown is not
// retryable: the operation may have happened.
func Retryable(err error) bool {
	if err == nil || errors.Is(err, ErrCommitUnknown) {
		return false
	}
	if errors.Is(err, ErrBudget) {
		return true
	}
	if _, retry := classify(err); retry {
		return true
	}
	// A connection lost before COMMIT: the transaction was rolled back.
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn)
}

// mysqlErrorNumber returns the number of a MySQL server error in err.
func mysqlErrorNumber(err error) (uint16, bool) {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number, true
	}
	return 0, false
}

// logRollback logs a failed attempt with the user and the MySQL error
// number. Server error messages are not logged: a duplicate key message
// quotes the row's values.
func logRollback(ctx context.Context, userId int64, attempt int, err error) {
	if n, ok := mysqlErrorNumber(err); ok {
		logx.WithContext(ctx).Errorf("userbox: user %d: attempt %d rolled back, mysql error %d", userId, attempt, n)
		return
	}
	logx.WithContext(ctx).Errorf("userbox: user %d: attempt %d rolled back: %v", userId, attempt, err)
}
