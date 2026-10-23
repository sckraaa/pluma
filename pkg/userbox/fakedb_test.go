package userbox

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// fakeServer is a database/sql driver that understands the statements of
// Run and keeps user_state, the journal and other writes in memory, with
// transactions: writes of a transaction become visible at COMMIT. Like
// MySQL after a deadlock, a statement that fails with an injected 1213 rolls
// the transaction back and leaves the connection in autocommit mode, where
// each statement commits at once. It logs every statement it receives.
type fakeServer struct {
	mu      sync.Mutex
	states  map[int64]State
	journal map[int64][]journalRow
	others  []string // committed statements of other kinds (fn's writes)
	log     []string // kinds of the statements received, in order
	inject  map[string][]injection
	// settings returned for Check: lock wait, flush, log_bin, format, sync.
	settings []driver.Value
}

// injection makes the next statement of a kind fail.
type injection struct {
	err error
	// afterApply: the statement takes effect, then its reply is lost (a
	// COMMIT applied by the server on a connection that breaks).
	afterApply bool
	// breakConn: the connection is unusable afterwards.
	breakConn bool
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		states:   map[int64]State{},
		journal:  map[int64][]journalRow{},
		inject:   map[string][]injection{},
		settings: []driver.Value{int64(2), int64(1), int64(1), "ROW", int64(1)},
	}
}

func (s *fakeServer) open(t *testing.T) *sql.DB {
	t.Helper()
	db := sql.OpenDB(fakeConnector{s})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func (s *fakeServer) failNext(kind string, in injection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inject[kind] = append(s.inject[kind], in)
}

func (s *fakeServer) statements() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

func (s *fakeServer) count(kind string) int {
	n := 0
	for _, k := range s.statements() {
		if k == kind {
			n++
		}
	}
	return n
}

func (s *fakeServer) state(userId int64) (State, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[userId]
	return st, ok
}

func (s *fakeServer) rows(userId int64) []journalRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]journalRow(nil), s.journal[userId]...)
}

func (s *fakeServer) otherWrites() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.others...)
}

// kind names a statement of Run, Read, Check or of a test's fn.
func kind(query string) string {
	switch {
	case query == "COMMIT":
		return "commit"
	case strings.HasPrefix(query, "INSERT INTO user_state"):
		return "lock"
	case query == lockedStateQuery:
		return "select_locked"
	case query == selectStateQuery:
		return "read"
	case strings.HasPrefix(query, insertJournal):
		return "journal"
	case strings.HasPrefix(query, "UPDATE user_state"):
		return "update_state"
	case strings.HasPrefix(query, "SELECT pts FROM user_state"):
		return "verify_lock"
	case strings.HasPrefix(query, "SELECT COUNT(*) FROM user_pts_updates"):
		return "verify_row"
	case strings.HasPrefix(query, "SELECT @@SESSION"):
		return "settings"
	case strings.HasPrefix(query, "SELECT"):
		return "select_other"
	}
	return "write_other"
}

type fakeConnector struct{ s *fakeServer }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{s: c.s}, nil }
func (c fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use the connector") }

// fakeConn is a connection; its transaction's writes wait in pending.
type fakeConn struct {
	s      *fakeServer
	bad    bool
	inTx   bool
	states map[int64]State
	rows   map[int64][]journalRow
	others []string
}

var (
	_ driver.ConnBeginTx    = (*fakeConn)(nil)
	_ driver.ExecerContext  = (*fakeConn)(nil)
	_ driver.QueryerContext = (*fakeConn)(nil)
	_ driver.Validator      = (*fakeConn)(nil)
)

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no prepare") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *fakeConn) IsValid() bool { return !c.bad }

func (c *fakeConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.bad {
		return nil, driver.ErrBadConn
	}
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.log = append(c.s.log, fmt.Sprintf("begin %d", opts.Isolation))
	c.inTx = true
	c.states = map[int64]State{}
	c.rows = map[int64][]journalRow{}
	c.others = nil
	return fakeTx{c}, nil
}

// applyLocked makes the pending writes visible. c.s.mu is held.
func (c *fakeConn) applyLocked() {
	for u, st := range c.states {
		c.s.states[u] = st
	}
	for u, rows := range c.rows {
		c.s.journal[u] = append(c.s.journal[u], rows...)
	}
	c.s.others = append(c.s.others, c.others...)
	c.discardLocked()
}

func (c *fakeConn) discardLocked() {
	c.inTx = false
	c.states, c.rows, c.others = nil, nil, nil
}

// current is the user's state as this connection sees it. c.s.mu is held.
func (c *fakeConn) current(userId int64) (State, bool) {
	if st, ok := c.states[userId]; ok {
		return st, true
	}
	st, ok := c.s.states[userId]
	return st, ok
}

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	n, err := c.run(query, args, nil)
	return driver.RowsAffected(n), err
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows := &fakeRows{}
	_, err := c.run(query, args, rows)
	return rows, err
}

// run executes a statement: rows is nil for Exec.
func (c *fakeConn) run(query string, args []driver.NamedValue, rows *fakeRows) (int64, error) {
	if c.bad {
		return 0, driver.ErrBadConn
	}
	s := c.s
	s.mu.Lock()
	defer s.mu.Unlock()
	k := kind(query)
	s.log = append(s.log, k)

	var in *injection
	if q := s.inject[k]; len(q) > 0 {
		in = &q[0]
		s.inject[k] = q[1:]
	}
	if in != nil && !in.afterApply {
		c.failLocked(in)
		return 0, in.err
	}

	autocommit := !c.inTx
	if autocommit {
		c.states = map[int64]State{}
		c.rows = map[int64][]journalRow{}
	}
	var affected int64
	switch k {
	case "commit":
		if c.inTx {
			c.applyLocked()
		}
	case "lock":
		u := args[0].Value.(int64)
		if _, ok := c.current(u); !ok {
			c.states[u] = State{Pts: BasePts, Date: args[1].Value.(int64)}
			affected = 1
		}
	case "select_locked", "read":
		u := args[0].Value.(int64)
		rows.cols = []string{"pts", "qts", "seq", "last_message_box_id", "date2"}
		if st, ok := c.current(u); ok {
			rows.data = [][]driver.Value{{int64(st.Pts), int64(st.Qts), int64(st.Seq), int64(st.LastMessageBoxId), st.Date}}
		}
	case "journal":
		for i := 0; i < len(args); i += 6 {
			u := args[i].Value.(int64)
			c.rows[u] = append(c.rows[u], journalRow{
				pts:        int32(args[i+1].Value.(int64)),
				ptsCount:   int32(args[i+2].Value.(int64)),
				updateType: int32(args[i+3].Value.(int64)),
				data:       args[i+4].Value.(string),
			})
		}
	case "update_state":
		u := args[3].Value.(int64)
		st, ok := c.current(u)
		if ok && int64(st.Pts) == args[4].Value.(int64) && int64(st.LastMessageBoxId) == args[5].Value.(int64) {
			st.Pts = int32(args[0].Value.(int64))
			st.LastMessageBoxId = int32(args[1].Value.(int64))
			st.Date = args[2].Value.(int64)
			c.states[u] = st
			affected = 1
		}
	case "verify_lock":
		rows.cols = []string{"pts"}
		if st, ok := c.current(args[0].Value.(int64)); ok {
			rows.data = [][]driver.Value{{int64(st.Pts)}}
		}
	case "verify_row":
		u := args[0].Value.(int64)
		var n int64
		for _, r := range s.journal[u] {
			if int64(r.pts) == args[1].Value.(int64) && int64(r.ptsCount) == args[2].Value.(int64) &&
				int64(r.updateType) == args[3].Value.(int64) && r.data == args[4].Value.(string) {
				n++
			}
		}
		rows.cols = []string{"count"}
		rows.data = [][]driver.Value{{n}}
	case "settings":
		rows.cols = []string{"a", "b", "c", "d", "e"}
		rows.data = [][]driver.Value{s.settings}
	case "select_other":
		rows.cols = []string{"v"}
	default:
		c.others = append(c.others, query)
		affected = 1
	}
	if autocommit {
		c.applyLocked()
	}
	if in != nil {
		c.failLocked(in)
		return 0, in.err
	}
	return affected, nil
}

// failLocked applies an injected failure: a deadlock rolls the transaction
// back on the server, a broken connection loses it. c.s.mu is held.
func (c *fakeConn) failLocked(in *injection) {
	if n, ok := mysqlErrorNumber(in.err); ok && n == errLockDeadlock {
		c.discardLocked() // InnoDB rolled back; autocommit from now on
	}
	if in.breakConn {
		c.bad = true
		c.discardLocked()
	}
}

type fakeTx struct{ c *fakeConn }

func (t fakeTx) Commit() error {
	c := t.c
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.log = append(c.s.log, "tx_commit")
	if c.bad {
		return driver.ErrBadConn
	}
	if c.inTx {
		c.applyLocked()
	}
	return nil
}

func (t fakeTx) Rollback() error {
	c := t.c
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.log = append(c.s.log, "rollback")
	if c.bad {
		return driver.ErrBadConn
	}
	c.discardLocked()
	return nil
}

type fakeRows struct {
	cols []string
	data [][]driver.Value
	i    int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.i])
	r.i++
	return nil
}
