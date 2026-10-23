package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Operations of pluma-admin on mq_outbox (ADR 0012, p. 5). The changes of
// inbox rows run in box transactions of their recipients, like the
// consumer's decisions, so they never fall between a check and a write.

// RowInfo is the metadata of a row, without its payload.
type RowInfo struct {
	Id           int64
	Initiator    int64
	Topic        string
	Recipient    int64
	Method       string
	State        userbox.EventState
	PublishCount int
	Redeliveries int
	Deferrals    int
	Attempts     int
	Replays      int
	DueAt        time.Time // zero when not set
	Error        string
	CreatedAt    time.Time
}

// ListFilter selects rows: zero fields select all.
type ListFilter struct {
	Id        int64
	State     *userbox.EventState
	Recipient int64
	Limit     int // default 100
}

// List returns rows by ascending id.
func List(ctx context.Context, db *sql.DB, f ListFilter) ([]RowInfo, error) {
	query := "SELECT id, initiator_id, topic, recipient_id, method, state, publish_count, redeliveries, deferrals, attempts, replays, due_at, error, created_at FROM mq_outbox WHERE 1 = 1"
	var args []any
	if f.Id != 0 {
		query += " AND id = ?"
		args = append(args, f.Id)
	}
	if f.State != nil {
		query += " AND state = ?"
		args = append(args, *f.State)
	}
	if f.Recipient != 0 {
		query += " AND recipient_id = ?"
		args = append(args, f.Recipient)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RowInfo
	for rows.Next() {
		var (
			r            RowInfo
			state        int8
			due, created int64
		)
		if err = rows.Scan(&r.Id, &r.Initiator, &r.Topic, &r.Recipient, &r.Method, &state, &r.PublishCount, &r.Redeliveries,
			&r.Deferrals, &r.Attempts, &r.Replays, &due, &r.Error, &created); err != nil {
			return nil, err
		}
		r.State = userbox.EventState(state)
		if due > 0 {
			r.DueAt = time.UnixMilli(due)
		}
		r.CreatedAt = time.UnixMilli(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Replay returns the dead and parked rows of a recipient, on every topic,
// to pending, with their replay count reset. It returns the rows replayed.
func Replay(ctx context.Context, db *sql.DB, recipient int64) (int, error) {
	l, err := lanes(ctx, db, "SELECT DISTINCT topic, recipient_id FROM mq_outbox WHERE recipient_id = ? AND state IN (?, ?)",
		recipient, userbox.EventDead, userbox.EventParked)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, lane := range l {
		n, err := replayLane(ctx, db, lane, false)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ErrNoRow: the row to discard does not exist.
var ErrNoRow = errors.New("outbox: no such row")

// Discard deletes a row in any state, on purpose: the event is never
// delivered. It returns what it deleted, for the operator's log. The other
// rows of an inbox lane stay as they are: replay them next (Replay).
func Discard(ctx context.Context, db *sql.DB, id int64) (RowInfo, error) {
	rows, err := List(ctx, db, ListFilter{Id: id})
	if err != nil {
		return RowInfo{}, err
	}
	if len(rows) == 0 {
		return RowInfo{}, ErrNoRow
	}
	info := rows[0]
	class, ok := userbox.TopicClassOf(info.Topic)
	if !ok {
		return info, fmt.Errorf("outbox: event %d: topic %q is not registered", id, info.Topic)
	}
	del := func(exec func(query string, args ...any) (sql.Result, error)) error {
		res, err := exec("DELETE FROM mq_outbox WHERE id = ?", id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			if err == nil {
				err = ErrNoRow
			}
			return err
		}
		return nil
	}
	if class == userbox.TopicPush {
		return info, del(func(q string, args ...any) (sql.Result, error) { return db.ExecContext(ctx, q, args...) })
	}
	_, err = userbox.Run(ctx, db, info.Recipient, func(b *userbox.Box) error {
		return del(b.Tx().Exec)
	})
	if err != nil {
		return info, fmt.Errorf("outbox: discard event %d: %w", id, err)
	}
	return info, nil
}

// IdMargin is how far RaiseIds lifts the ids of new events.
const IdMargin = 1_000_000_000

// RaiseIds lifts the AUTO_INCREMENT of mq_outbox by margin above the
// highest id the table has given, so no id is given again: after MySQL is
// restored from a backup, copies in Kafka still carry the ids given after
// it (ADR 0012, p. 11). It never lowers the counter, and returns the new
// one. Run while msg is stopped.
func RaiseIds(ctx context.Context, db *sql.DB, margin int64) (int64, error) {
	if margin < 0 {
		return 0, fmt.Errorf("outbox: negative margin %d", margin)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	// The cached statistics of information_schema may be behind the counter.
	if _, err = conn.ExecContext(ctx, "SET SESSION information_schema_stats_expiry = 0"); err != nil {
		return 0, err
	}
	var counter, maxId sql.NullInt64
	if err = conn.QueryRowContext(ctx,
		"SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'mq_outbox'").Scan(&counter); err != nil {
		return 0, fmt.Errorf("outbox: read the id counter: %w", err)
	}
	if err = conn.QueryRowContext(ctx, "SELECT MAX(id) FROM mq_outbox").Scan(&maxId); err != nil {
		return 0, err
	}
	next := max(counter.Int64, maxId.Int64+1, 1) + margin
	if _, err = conn.ExecContext(ctx, "ALTER TABLE mq_outbox AUTO_INCREMENT = "+strconv.FormatInt(next, 10)); err != nil {
		return 0, fmt.Errorf("outbox: raise the id counter: %w", err)
	}
	return next, nil
}
