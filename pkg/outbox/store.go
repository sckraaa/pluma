package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// row is a pending row as a round selects it: without its payload.
type row struct {
	id           int64
	revision     int
	topic        string
	recipient    int64
	initiator    int64
	method       string
	trace        string
	createdAt    int64 // unix ms
	size         int   // bytes of the payload
	redeliveries int
}

// ack is an inbox row the broker took.
type ack struct {
	id        int64
	revision  int
	partition int32
	offset    int64
	dueAt     int64 // when to check that the copy was consumed
}

// ref names a row in the state the relay saw it.
type ref struct {
	id       int64
	revision int
}

// finish is what a round's answers change: inbox rows published, push rows
// deleted (taken by the broker, or refused for good).
type finish struct {
	published []ack
	deleted   []ref
}

func (f finish) empty() bool { return len(f.published) == 0 && len(f.deleted) == 0 }

// published is an inbox row whose copy the redelivery timer checks.
type published struct {
	id           int64
	revision     int
	topic        string
	partition    int32
	offset       int64
	redeliveries int
}

// Lane is the events of one recipient on one topic, applied in id order.
type Lane struct {
	Topic     string
	Recipient int64
}

// counts are the rows by state and the age of the oldest unapplied one.
type counts struct {
	rows   map[userbox.EventState]int64
	oldest int64 // created_at (unix ms) of the oldest pending or published row; 0 without one
}

// store is the database side of the relay.
type store interface {
	// Lead tries to take the relay's lock; Leading tells whether the lock
	// is still held, on the same connection; Resign releases it.
	Lead(ctx context.Context) (bool, error)
	Leading(ctx context.Context) (bool, error)
	Resign()

	// Pending returns up to limit pending rows of topics by ascending id,
	// skipping skip.
	Pending(ctx context.Context, topics []string, skip map[int64]bool, limit int) ([]row, error)
	// Payloads returns the payloads of rows.
	Payloads(ctx context.Context, ids []int64) (map[int64][]byte, error)
	// Finish records the answers of a round in one transaction.
	Finish(ctx context.Context, f finish) error
	// Dead makes an inbox row the broker refused for good dead, in a box
	// transaction of its recipient.
	Dead(ctx context.Context, r row, reason string) error

	// ResetPublished returns every published row to pending.
	ResetPublished(ctx context.Context) (int, error)
	// DueForCheck returns published rows due for the redelivery check.
	DueForCheck(ctx context.Context, now int64, limit int) ([]published, error)
	// Redeliver returns a published row to pending; Postpone moves its
	// next check. Both change the row only in the revision given.
	Redeliver(ctx context.Context, r ref) (bool, error)
	Postpone(ctx context.Context, r ref, dueAt int64) error
	// DeadLanes returns the lanes with a dead row due for replay at now
	// (every lane with a dead row when now is 0).
	DeadLanes(ctx context.Context, now int64) ([]Lane, error)
	// ReplayLane returns the dead and parked rows of a lane to pending.
	ReplayLane(ctx context.Context, l Lane, auto bool) (int, error)
	Counts(ctx context.Context) (counts, error)
}

// sqlStore is the store on MySQL. The relay's lock lives on a connection
// of its own, where the rounds select too; everything else runs on the
// pool in READ COMMITTED and changes rows by primary key only, so the
// inserts of box transactions never wait for the relay.
type sqlStore struct {
	db   *sql.DB
	conn *sql.Conn
}

func newSQLStore(db *sql.DB) *sqlStore { return &sqlStore{db: db} }

func (s *sqlStore) Lead(ctx context.Context) (bool, error) {
	if s.conn == nil {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return false, err
		}
		if _, err = conn.ExecContext(ctx, "SET SESSION transaction_isolation = 'READ-COMMITTED'"); err != nil {
			_ = conn.Close()
			return false, err
		}
		s.conn = conn
	}
	var got sql.NullInt64
	if err := s.conn.QueryRowContext(ctx, "SELECT GET_LOCK("+lockName+", 0)").Scan(&got); err != nil {
		s.Resign()
		return false, err
	}
	return got.Valid && got.Int64 == 1, nil
}

func (s *sqlStore) Leading(ctx context.Context) (bool, error) {
	if s.conn == nil {
		return false, nil
	}
	var ok sql.NullBool
	if err := s.conn.QueryRowContext(ctx, "SELECT IS_USED_LOCK("+lockName+") = CONNECTION_ID()").Scan(&ok); err != nil {
		s.Resign()
		return false, err
	}
	return ok.Valid && ok.Bool, nil
}

func (s *sqlStore) Resign() {
	if s.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	_, _ = s.conn.ExecContext(ctx, "DO RELEASE_LOCK("+lockName+")")
	_ = s.conn.Close()
	s.conn = nil
}

func (s *sqlStore) Pending(ctx context.Context, topics []string, skip map[int64]bool, limit int) ([]row, error) {
	if s.conn == nil {
		return nil, errNotLeader
	}
	args := make([]any, 0, len(topics)+2)
	args = append(args, userbox.EventPending)
	for _, t := range topics {
		args = append(args, t)
	}
	args = append(args, limit+len(skip))
	rows, err := s.conn.QueryContext(ctx, pendingQuery(len(topics)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.revision, &r.topic, &r.recipient, &r.initiator, &r.method, &r.trace, &r.createdAt, &r.size, &r.redeliveries); err != nil {
			return nil, err
		}
		if !skip[r.id] && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// pendingQuery selects the pending rows of n topics.
func pendingQuery(n int) string {
	return "SELECT id, revision, topic, recipient_id, initiator_id, method, trace, created_at, LENGTH(payload), redeliveries FROM mq_outbox" +
		" WHERE state = ? AND topic IN (" + inList(n) + ") ORDER BY id LIMIT ?"
}

func (s *sqlStore) Payloads(ctx context.Context, ids []int64) (map[int64][]byte, error) {
	if s.conn == nil {
		return nil, errNotLeader
	}
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.conn.QueryContext(ctx, "SELECT id, payload FROM mq_outbox WHERE id IN ("+inList(len(ids))+")", args...) //nolint:gosec // only placeholders are joined into the query
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]byte, len(ids))
	for rows.Next() {
		var (
			id      int64
			payload []byte
		)
		if err = rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		out[id] = payload
	}
	return out, rows.Err()
}

// Statements of the finisher and the timer: the rows by primary key, in
// the revision the relay saw (the derived table first: eq_ref on PRIMARY,
// whatever the size of the table).
const (
	finishPublishedRow = "ROW(?, ?, ?, ?, ?)"
	finishPublished    = "UPDATE (VALUES %s) AS v (id, revision, kp, ko, due) STRAIGHT_JOIN mq_outbox o ON o.id = v.id AND o.revision = v.revision" +
		" SET o.state = 1, o.revision = o.revision + 1, o.publish_count = o.publish_count + 1, o.kafka_partition = v.kp, o.kafka_offset = v.ko, o.due_at = v.due"
	finishDeletedRow = "ROW(?, ?)"
	finishDeleted    = "DELETE o FROM (VALUES %s) AS v (id, revision) STRAIGHT_JOIN mq_outbox o ON o.id = v.id AND o.revision = v.revision"
	redeliverQuery   = "UPDATE mq_outbox SET state = 0, revision = revision + 1, redeliveries = redeliveries + 1 WHERE id = ? AND revision = ? AND state = 1"
	postponeQuery    = "UPDATE mq_outbox SET due_at = ? WHERE id = ? AND revision = ?"
	resetQuery       = "UPDATE (VALUES %s) AS v (id, revision) STRAIGHT_JOIN mq_outbox o ON o.id = v.id AND o.revision = v.revision" +
		" SET o.state = 0, o.revision = o.revision + 1 WHERE o.state = 1"
)

func values(row string, n int) string {
	return strings.TrimSuffix(strings.Repeat(row+", ", n), ", ")
}

func (s *sqlStore) Finish(ctx context.Context, f finish) error {
	if f.empty() {
		return nil
	}
	return retryLocks(ctx, func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if len(f.published) > 0 {
			args := make([]any, 0, 5*len(f.published))
			for _, a := range f.published {
				args = append(args, a.id, a.revision, a.partition, a.offset, a.dueAt)
			}
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(finishPublished, values(finishPublishedRow, len(f.published))), args...); err != nil {
				return err
			}
		}
		if len(f.deleted) > 0 {
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(finishDeleted, values(finishDeletedRow, len(f.deleted))), refArgs(f.deleted)...); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func refArgs(refs []ref) []any {
	args := make([]any, 0, 2*len(refs))
	for _, r := range refs {
		args = append(args, r.id, r.revision)
	}
	return args
}

func (s *sqlStore) Dead(ctx context.Context, r row, reason string) error {
	_, err := userbox.Run(ctx, s.db, r.recipient, func(b *userbox.Box) error {
		_, err := b.Tx().Exec("UPDATE mq_outbox SET state = ?, revision = revision + 1, error = ?, due_at = ? WHERE id = ? AND revision = ? AND state = ?",
			userbox.EventDead, reason, nowMs()+userbox.ReplayDelay(0).Milliseconds(), r.id, r.revision, userbox.EventPending)
		return err
	})
	return err
}

// readRefs reads ids and revisions with a query on the pool, in READ
// COMMITTED: a plain read, no locks.
func (s *sqlStore) readRefs(ctx context.Context, query string, args ...any) ([]ref, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ref
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.id, &r.revision); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlStore) ResetPublished(ctx context.Context) (int, error) {
	total := 0
	after := int64(0)
	for {
		refs, err := s.readRefs(ctx, "SELECT id, revision FROM mq_outbox WHERE state = ? AND id > ? ORDER BY id LIMIT 1000", userbox.EventPublished, after)
		if err != nil || len(refs) == 0 {
			return total, err
		}
		after = refs[len(refs)-1].id
		err = retryLocks(ctx, func() error {
			res, err := s.db.ExecContext(ctx, fmt.Sprintf(resetQuery, values(finishDeletedRow, len(refs))), refArgs(refs)...)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			total += int(n)
			return err
		})
		if err != nil {
			return total, err
		}
	}
}

func (s *sqlStore) DueForCheck(ctx context.Context, now int64, limit int) ([]published, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, revision, topic, kafka_partition, kafka_offset, redeliveries FROM mq_outbox WHERE state = ? AND due_at <= ? ORDER BY id LIMIT ?",
		userbox.EventPublished, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []published
	for rows.Next() {
		var p published
		if err = rows.Scan(&p.id, &p.revision, &p.topic, &p.partition, &p.offset, &p.redeliveries); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqlStore) Redeliver(ctx context.Context, r ref) (bool, error) {
	var n int64
	err := retryLocks(ctx, func() error {
		res, err := s.db.ExecContext(ctx, redeliverQuery, r.id, r.revision)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n == 1, err
}

func (s *sqlStore) Postpone(ctx context.Context, r ref, dueAt int64) error {
	return retryLocks(ctx, func() error {
		_, err := s.db.ExecContext(ctx, postponeQuery, dueAt, r.id, r.revision)
		return err
	})
}

func (s *sqlStore) DeadLanes(ctx context.Context, now int64) ([]Lane, error) {
	query := "SELECT DISTINCT topic, recipient_id FROM mq_outbox WHERE state = ?"
	args := []any{userbox.EventDead}
	if now > 0 {
		query += " AND due_at <= ?"
		args = append(args, now)
	}
	return lanes(ctx, s.db, query, args...)
}

func lanes(ctx context.Context, db *sql.DB, query string, args ...any) ([]Lane, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lane
	for rows.Next() {
		var l Lane
		if err = rows.Scan(&l.Topic, &l.Recipient); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqlStore) ReplayLane(ctx context.Context, l Lane, auto bool) (int, error) {
	return replayLane(ctx, s.db, l, auto)
}

// replayLane returns the dead and parked rows of a lane to pending, in a
// box transaction of the recipient: under the lock its consumer decides
// under, so a decision to park cannot fall between the read and the write.
// An automatic replay counts in replays (the next one waits longer); the
// operator's starts the count again.
func replayLane(ctx context.Context, db *sql.DB, l Lane, auto bool) (int, error) {
	n := 0
	_, err := userbox.Run(ctx, db, l.Recipient, func(b *userbox.Box) error {
		n = 0
		var ids []int64
		if err := b.Tx().QueryRowsPartial(&ids, "SELECT id FROM mq_outbox WHERE topic = ? AND recipient_id = ? AND state IN (?, ?)",
			l.Topic, l.Recipient, userbox.EventDead, userbox.EventParked); err != nil || len(ids) == 0 {
			return err
		}
		replays := "0"
		if auto {
			replays = "replays + 1"
		}
		args := make([]any, 0, len(ids)+1)
		args = append(args, userbox.EventPending)
		for _, id := range ids {
			args = append(args, id)
		}
		if _, err := b.Tx().Exec("UPDATE mq_outbox SET state = ?, revision = revision + 1, replays = "+replays+" WHERE id IN ("+inList(len(ids))+")", args...); err != nil {
			return err
		}
		n = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if n > 0 {
		Notify()
	}
	return n, nil
}

func (s *sqlStore) Counts(ctx context.Context) (counts, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT state, COUNT(*), MIN(created_at) FROM mq_outbox GROUP BY state")
	if err != nil {
		return counts{}, err
	}
	defer rows.Close()
	c := counts{rows: map[userbox.EventState]int64{}}
	for rows.Next() {
		var (
			st     int8
			n, min int64
		)
		if err = rows.Scan(&st, &n, &min); err != nil {
			return counts{}, err
		}
		state := userbox.EventState(st)
		c.rows[state] = n
		if (state == userbox.EventPending || state == userbox.EventPublished) && (c.oldest == 0 || min < c.oldest) {
			c.oldest = min
		}
	}
	return c, rows.Err()
}

// retryLocks runs f again after a deadlock or a lock wait timeout, up to
// five times: nothing it changed was kept, and nothing is published again.
func retryLocks(ctx context.Context, f func() error) error {
	var err error
	for range 5 {
		if err = f(); err == nil || !lockError(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func lockError(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1213 || me.Number == 1205)
}
