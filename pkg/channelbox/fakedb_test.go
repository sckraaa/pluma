package channelbox

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
	"time"

	"github.com/go-sql-driver/mysql"
)

// fakeServer is a database/sql driver that understands the statements a
// user's box transaction with channels sends, keeps the channels and their
// journal in memory, with transactions (writes become visible at COMMIT)
// and row locks (SELECT ... FOR UPDATE, INSERT and UPDATE lock a row until
// the transaction ends), as InnoDB under READ COMMITTED. A plain SELECT
// reads the committed row without waiting. It logs the kind of every
// statement.
type fakeServer struct {
	mu       sync.Mutex
	unlocked *sync.Cond
	users    map[int64]bool
	channels map[int64]State
	journal  map[int64][]journalRow
	locks    map[string]*fakeConn
	log      []string
	inject   map[string][]injection
}

// injection makes the next statement of a kind fail.
type injection struct {
	err error
	// afterApply: the statement takes effect, then its reply is lost.
	afterApply bool
}

func newFakeServer() *fakeServer {
	s := &fakeServer{
		users:    map[int64]bool{},
		channels: map[int64]State{},
		journal:  map[int64][]journalRow{},
		locks:    map[string]*fakeConn{},
		inject:   map[string][]injection{},
	}
	s.unlocked = sync.NewCond(&s.mu)
	return s
}

func (s *fakeServer) open(t *testing.T, conns int) *sql.DB {
	t.Helper()
	db := sql.OpenDB(fakeConnector{s})
	db.SetMaxOpenConns(conns)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func (s *fakeServer) failNext(kind string, in injection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inject[kind] = append(s.inject[kind], in)
}

func (s *fakeServer) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.log {
		if k == kind {
			n++
		}
	}
	return n
}

func (s *fakeServer) channel(id int64) (State, []journalRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.channels[id]
	return st, append([]journalRow(nil), s.journal[id]...), ok
}

func (s *fakeServer) addChannel(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels[id] = State{Pts: BasePts}
}

// kind names a statement.
func kind(query string) string {
	switch {
	case query == "COMMIT":
		return "commit"
	case strings.HasPrefix(query, "INSERT INTO user_state"):
		return "lock_user"
	case strings.HasPrefix(query, "SELECT pts, qts, seq, last_message_box_id, date2 FROM user_state"):
		return "select_user"
	case strings.HasPrefix(query, "SELECT pts, seq FROM user_state"):
		return "verify_user"
	case strings.HasPrefix(query, "SELECT pts, last_message_id, date2 FROM channels") && strings.HasSuffix(query, " FOR UPDATE"):
		return "lock_channel"
	case strings.HasPrefix(query, "SELECT pts, last_message_id, date2 FROM channels"):
		return "read_channel"
	case strings.HasPrefix(query, "SELECT pts FROM channels"):
		return "verify_channel"
	case strings.HasPrefix(query, "SELECT COUNT(*) FROM channel_pts_updates"):
		return "verify_row"
	case query == createQuery:
		return "create"
	case query == updateQuery:
		return "update"
	case strings.HasPrefix(query, insertJournal):
		return "journal"
	}
	return "other"
}

type fakeConnector struct{ s *fakeServer }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{s: c.s}, nil }
func (c fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use the connector") }

// fakeConn is a connection; its transaction's writes wait in pending.
type fakeConn struct {
	s        *fakeServer
	inTx     bool
	users    map[int64]bool
	channels map[int64]State
	journal  map[int64][]journalRow
}

var (
	_ driver.ConnBeginTx    = (*fakeConn)(nil)
	_ driver.ExecerContext  = (*fakeConn)(nil)
	_ driver.QueryerContext = (*fakeConn)(nil)
)

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no prepare") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.inTx = true
	c.users, c.channels, c.journal = map[int64]bool{}, map[int64]State{}, map[int64][]journalRow{}
	return fakeTx{c}, nil
}

// endLocked ends the transaction, applying its writes if commit, and
// releases its locks. c.s.mu is held.
func (c *fakeConn) endLocked(commit bool) {
	if !c.inTx {
		return
	}
	if commit {
		for id := range c.users {
			c.s.users[id] = true
		}
		for id, st := range c.channels {
			c.s.channels[id] = st
		}
		for id, rows := range c.journal {
			c.s.journal[id] = append(c.s.journal[id], rows...)
		}
	}
	c.inTx = false
	c.users, c.channels, c.journal = nil, nil, nil
	for k, owner := range c.s.locks {
		if owner == c {
			delete(c.s.locks, k)
		}
	}
	c.s.unlocked.Broadcast()
}

// lockLocked takes the row lock key for the transaction, waiting for
// another holder; it gives up with 1205 after the lock wait timeout. c.s.mu
// is held.
func (c *fakeConn) lockLocked(key string) error {
	if !c.inTx {
		return nil
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		owner, held := c.s.locks[key]
		if !held || owner == c {
			c.s.locks[key] = c
			return nil
		}
		if time.Now().After(deadline) {
			return &mysql.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"}
		}
		// Woken by any release; a timer wakes the wait at the deadline.
		t := time.AfterFunc(time.Until(deadline), func() {
			c.s.mu.Lock()
			c.s.unlocked.Broadcast()
			c.s.mu.Unlock()
		})
		c.s.unlocked.Wait()
		t.Stop()
	}
}

// channelLocked is the channel as this connection sees it. c.s.mu is held.
func (c *fakeConn) channelLocked(id int64) (State, bool) {
	if c.inTx {
		if st, ok := c.channels[id]; ok {
			return st, true
		}
	}
	st, ok := c.s.channels[id]
	return st, ok
}

func (c *fakeConn) journalLocked(id int64) []journalRow {
	rows := append([]journalRow(nil), c.s.journal[id]...)
	if c.inTx {
		rows = append(rows, c.journal[id]...)
	}
	return rows
}

func (c *fakeConn) injectedLocked(k string) (injection, bool) {
	list := c.s.inject[k]
	if len(list) == 0 {
		return injection{}, false
	}
	c.s.inject[k] = list[1:]
	return list[0], true
}

func duplicate() error { return &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"} }

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	k := kind(query)
	c.s.log = append(c.s.log, k)
	in, injected := c.injectedLocked(k)
	if injected && !in.afterApply {
		return nil, in.err
	}
	v := func(i int) int64 { return asInt(args[i].Value) }
	var affected int64
	switch k {
	case "commit":
		c.endLocked(true)
	case "lock_user":
		if err := c.lockLocked(fmt.Sprintf("u%d", v(0))); err != nil {
			return nil, err
		}
		c.users[v(0)] = true
		affected = 1
	case "create":
		id := v(0)
		if err := c.lockLocked(fmt.Sprintf("c%d", id)); err != nil {
			return nil, err
		}
		if _, ok := c.channelLocked(id); ok {
			return nil, duplicate()
		}
		c.channels[id] = State{Pts: int32(v(10)), Date: v(12)}
		affected = 1
	case "update":
		id := v(3)
		if err := c.lockLocked(fmt.Sprintf("c%d", id)); err != nil {
			return nil, err
		}
		st, ok := c.channelLocked(id)
		if ok && st.Pts == int32(v(4)) && st.LastMessageId == int32(v(5)) {
			c.channels[id] = State{Pts: int32(v(0)), LastMessageId: int32(v(1)), Date: v(2)}
			affected = 1
		}
	case "journal":
		for i := 0; i < len(args); i += 6 {
			id, pts := v(i), int32(v(i+1))
			for _, r := range c.journalLocked(id) {
				if r.pts == pts {
					return nil, duplicate()
				}
			}
			c.journal[id] = append(c.journal[id], journalRow{pts: pts, ptsCount: int32(v(i + 2)), updateType: int32(v(i + 3)), data: args[i+4].Value.(string)})
			affected++
		}
	default:
		return nil, fmt.Errorf("fake: unexpected statement %q", query)
	}
	if injected {
		return nil, in.err
	}
	return driver.RowsAffected(affected), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	k := kind(query)
	c.s.log = append(c.s.log, k)
	if in, ok := c.injectedLocked(k); ok {
		return nil, in.err
	}
	id := asInt(args[0].Value)
	switch k {
	case "select_user":
		if err := c.lockLocked(fmt.Sprintf("u%d", id)); err != nil {
			return nil, err
		}
		return rowsOf([]string{"pts", "qts", "seq", "last_message_box_id", "date2"}, int64(1), int64(0), int64(1), int64(0), int64(0)), nil
	case "verify_user":
		if err := c.lockLocked(fmt.Sprintf("u%d", id)); err != nil {
			return nil, err
		}
		if !c.s.users[id] {
			return rowsOf([]string{"pts", "seq"}), nil
		}
		return rowsOf([]string{"pts", "seq"}, int64(1), int64(1)), nil
	case "lock_channel":
		if err := c.lockLocked(fmt.Sprintf("c%d", id)); err != nil {
			return nil, err
		}
		fallthrough
	case "read_channel", "verify_channel":
		st, ok := c.channelLocked(id)
		if k != "lock_channel" {
			st, ok = c.s.channels[id] // committed, without waiting
		}
		if !ok {
			return rowsOf([]string{"pts", "last_message_id", "date2"}), nil
		}
		if k == "verify_channel" {
			return rowsOf([]string{"pts"}, int64(st.Pts)), nil
		}
		return rowsOf([]string{"pts", "last_message_id", "date2"}, int64(st.Pts), int64(st.LastMessageId), st.Date), nil
	case "verify_row":
		n := int64(0)
		for _, r := range c.journalLocked(id) {
			if r.pts == int32(asInt(args[1].Value)) && r.ptsCount == int32(asInt(args[2].Value)) &&
				r.updateType == int32(asInt(args[3].Value)) && r.data == args[4].Value.(string) {
				n++
			}
		}
		return rowsOf([]string{"COUNT(*)"}, n), nil
	}
	return nil, fmt.Errorf("fake: unexpected query %q", query)
}

func asInt(v driver.Value) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	}
	panic(fmt.Sprintf("fake: argument %T", v))
}

type fakeTx struct{ c *fakeConn }

// Commit ends a transaction COMMIT has not ended (database/sql's own call
// after the COMMIT statement finds none and does nothing).
func (t fakeTx) Commit() error {
	t.c.s.mu.Lock()
	defer t.c.s.mu.Unlock()
	t.c.endLocked(true)
	return nil
}

func (t fakeTx) Rollback() error {
	t.c.s.mu.Lock()
	defer t.c.s.mu.Unlock()
	t.c.endLocked(false)
	return nil
}

// fakeRows is a result with one row, or none.
type fakeRows struct {
	cols []string
	row  []driver.Value
	done bool
}

func rowsOf(cols []string, row ...driver.Value) *fakeRows {
	return &fakeRows{cols: cols, row: row, done: len(row) == 0}
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.row)
	r.done = true
	return nil
}
