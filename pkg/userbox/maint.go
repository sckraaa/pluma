package userbox

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// Maintenance of user_state (ADR 0011, pp. 10-11): commands of pluma-admin
// that run while the services that write boxes (msg, biz) are stopped. They
// are the only writers of user_state besides Run.

// raiseExistingQuery lifts the existing rows above everything written:
// pts above the journal, seq above the seq journal, box ids above the
// messages, all by the margin.
const raiseExistingQuery = "UPDATE user_state s" +
	" LEFT JOIN (SELECT user_id, MAX(pts) AS max_pts FROM user_pts_updates GROUP BY user_id) p ON p.user_id = s.user_id" +
	" LEFT JOIN (SELECT user_id, MAX(seq) AS max_seq FROM user_seq_updates GROUP BY user_id) q ON q.user_id = s.user_id" +
	" LEFT JOIN (SELECT user_id, MAX(user_message_box_id) AS max_box_id FROM messages GROUP BY user_id) m ON m.user_id = s.user_id" +
	" SET s.pts = GREATEST(s.pts, COALESCE(p.max_pts, 0)) + ?," +
	" s.seq = GREATEST(s.seq, COALESCE(q.max_seq, 0)) + ?," +
	" s.last_message_box_id = GREATEST(s.last_message_box_id, COALESCE(m.max_box_id, 0)) + ?," +
	" s.date2 = UNIX_TIMESTAMP()"

// insertMissingQuery adds the rows of users that have none, as the
// migration does (migrate-20261022.sql).
const insertMissingQuery = "INSERT INTO user_state (user_id, pts, seq, last_message_box_id, date2)" +
	" SELECT u.id, GREATEST(1, COALESCE(p.max_pts, 0)) + ?, GREATEST(1, COALESCE(q.max_seq, 0)) + ?, COALESCE(m.max_box_id, 0) + ?, UNIX_TIMESTAMP()" +
	" FROM (SELECT id FROM users UNION SELECT user_id FROM user_pts_updates UNION SELECT user_id FROM user_seq_updates UNION SELECT user_id FROM messages) u" +
	" LEFT JOIN (SELECT user_id, MAX(pts) AS max_pts FROM user_pts_updates GROUP BY user_id) p ON p.user_id = u.id" +
	" LEFT JOIN (SELECT user_id, MAX(seq) AS max_seq FROM user_seq_updates GROUP BY user_id) q ON q.user_id = u.id" +
	" LEFT JOIN (SELECT user_id, MAX(user_message_box_id) AS max_box_id FROM messages GROUP BY user_id) m ON m.user_id = u.id" +
	" LEFT JOIN user_state s ON s.user_id = u.id" +
	" WHERE s.user_id IS NULL"

// Raise lifts the state of every user (userId 0) or of one user by margin
// above the highest pts, seq and box id written, and adds the missing rows
// the same way: after a rollback to a version that kept the counters in
// Redis, or a restore of MySQL from a backup, clients may have seen values
// above the rows. A client ahead in seq would take every next update with
// seq for one it has and drop it without asking for a difference. It
// returns the rows changed and added.
func Raise(ctx context.Context, db *sql.DB, margin int32, userId int64) (raised, added int64, err error) {
	if margin < 0 {
		return 0, 0, fmt.Errorf("userbox: negative margin %d", margin)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	raise, insert, args := raiseExistingQuery, insertMissingQuery, []any{margin, margin, margin}
	if userId != 0 {
		raise += " WHERE s.user_id = ?"
		insert += " AND u.id = ?"
		args = append(args, userId)
	}
	r, err := tx.ExecContext(ctx, raise, args...)
	if err != nil {
		return 0, 0, fmt.Errorf("userbox: raise user_state: %w", err)
	}
	if raised, err = r.RowsAffected(); err != nil {
		return 0, 0, err
	}
	if r, err = tx.ExecContext(ctx, insert, args...); err != nil {
		return 0, 0, fmt.Errorf("userbox: add user_state rows: %w", err)
	}
	if added, err = r.RowsAffected(); err != nil {
		return 0, 0, err
	}
	return raised, added, tx.Commit()
}

// Keys of the counters a version before task 002 kept in Redis (idgen).
const (
	LegacyPtsKeyPrefix        = "pts_updates_ngen_"
	LegacyMessageBoxKeyPrefix = "message_box_ngen_"
)

// ExportLegacy writes the counters of every user_state row, plus margin, to
// the Redis keys of a version before task 002 with set (SET, not INCR), so
// that version, restored, continues above everything written. It returns
// the number of users.
func ExportLegacy(ctx context.Context, db *sql.DB, margin int32, set func(ctx context.Context, key, value string) error) (int, error) {
	if margin < 0 {
		return 0, fmt.Errorf("userbox: negative margin %d", margin)
	}
	rows, err := db.QueryContext(ctx, "SELECT user_id, pts, last_message_box_id FROM user_state ORDER BY user_id")
	if err != nil {
		return 0, fmt.Errorf("userbox: read user_state: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var (
			userId     int64
			pts, boxId int64
		)
		if err = rows.Scan(&userId, &pts, &boxId); err != nil {
			return n, err
		}
		u := strconv.FormatInt(userId, 10)
		if err = set(ctx, LegacyPtsKeyPrefix+u, strconv.FormatInt(pts+int64(margin), 10)); err != nil {
			return n, err
		}
		if err = set(ctx, LegacyMessageBoxKeyPrefix+u, strconv.FormatInt(boxId+int64(margin), 10)); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}
