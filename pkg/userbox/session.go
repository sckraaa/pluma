package userbox

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// errPrepare: a prepared statement would run outside the recorder, so its
// errors would not stop the transaction.
var errPrepare = errors.New("userbox: prepared statements are not supported in a box transaction")

// recorder is the go-zero session of a box transaction. It keeps the first
// error of any statement and from then on sends nothing to the server,
// returning that error again. A row-reading statement that finds no row
// (sql.ErrNoRows) is a result, not a failure.
//
// Run never commits a transaction with a recorded error, whatever fn
// returns: after a deadlock (1213) InnoDB has already rolled the transaction
// back and released its locks, the connection is in autocommit, and every
// further statement of the same *sql.Tx would commit on its own, without the
// user_state lock. After a lock wait timeout (1205) only the statement is
// rolled back (innodb_rollback_on_timeout=OFF), and the explicit ROLLBACK
// ends the transaction.
type recorder struct {
	sess sqlx.Session
	ctx  context.Context // for the methods without a context

	mu  sync.Mutex
	err error
}

var _ sqlx.Session = (*recorder)(nil)

func newRecorder(ctx context.Context, sess sqlx.Session) *recorder {
	return &recorder{sess: sess, ctx: ctx}
}

// failed returns the first error of the transaction's statements.
func (r *recorder) failed() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *recorder) do(noRowsIsResult bool, f func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	err := f()
	if err != nil && (!noRowsIsResult || !errors.Is(err, sql.ErrNoRows)) {
		r.err = err
	}
	return err
}

func (r *recorder) Exec(query string, args ...any) (sql.Result, error) {
	return r.ExecCtx(r.ctx, query, args...)
}

func (r *recorder) ExecCtx(ctx context.Context, query string, args ...any) (res sql.Result, err error) {
	err = r.do(false, func() error {
		res, err = r.sess.ExecCtx(ctx, query, args...)
		return err
	})
	return res, err
}

func (r *recorder) Prepare(query string) (sqlx.StmtSession, error) {
	return r.PrepareCtx(r.ctx, query)
}

func (r *recorder) PrepareCtx(context.Context, string) (sqlx.StmtSession, error) {
	return nil, r.do(false, func() error { return errPrepare })
}

func (r *recorder) QueryRow(v any, query string, args ...any) error {
	return r.QueryRowCtx(r.ctx, v, query, args...)
}

func (r *recorder) QueryRowCtx(ctx context.Context, v any, query string, args ...any) error {
	return r.do(true, func() error { return r.sess.QueryRowCtx(ctx, v, query, args...) })
}

func (r *recorder) QueryRowPartial(v any, query string, args ...any) error {
	return r.QueryRowPartialCtx(r.ctx, v, query, args...)
}

func (r *recorder) QueryRowPartialCtx(ctx context.Context, v any, query string, args ...any) error {
	return r.do(true, func() error { return r.sess.QueryRowPartialCtx(ctx, v, query, args...) })
}

func (r *recorder) QueryRows(v any, query string, args ...any) error {
	return r.QueryRowsCtx(r.ctx, v, query, args...)
}

func (r *recorder) QueryRowsCtx(ctx context.Context, v any, query string, args ...any) error {
	return r.do(false, func() error { return r.sess.QueryRowsCtx(ctx, v, query, args...) })
}

func (r *recorder) QueryRowsPartial(v any, query string, args ...any) error {
	return r.QueryRowsPartialCtx(r.ctx, v, query, args...)
}

func (r *recorder) QueryRowsPartialCtx(ctx context.Context, v any, query string, args ...any) error {
	return r.do(false, func() error { return r.sess.QueryRowsPartialCtx(ctx, v, query, args...) })
}
