package userbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// BasePts is the pts of a box nobody has written to yet (no user_state row):
// its first update gets pts BasePts+1, and updates.getState never returns 0.
const BasePts = 1

// BaseSeq is the seq of an account no update with seq was sent to yet: the
// first such update gets seq BaseSeq+1, which a client holding BaseSeq
// applies without a gap. 0 is not used: an update with seq 0 is one
// without order (ADR 0019).
const BaseSeq = 1

// State is the committed state of a user's common message box: a row of
// user_state.
type State struct {
	Pts              int32 // last committed pts
	Qts              int32 // reserved: does not change yet
	Seq              int32 // of the last update with seq (Box.AddSeqUpdate)
	LastMessageBoxId int32 // last issued user_message_box_id
	Date             int64 // unix time of the last change (date2)
}

// baseState is the state of a user without a user_state row.
func baseState() State { return State{Pts: BasePts, Seq: BaseSeq} }

// stateRow is a user_state row as go-zero scans it.
type stateRow struct {
	Pts              int32 `db:"pts"`
	Qts              int32 `db:"qts"`
	Seq              int32 `db:"seq"`
	LastMessageBoxId int32 `db:"last_message_box_id"`
	Date2            int64 `db:"date2"`
}

func (r stateRow) state() State {
	return State{Pts: r.Pts, Qts: r.Qts, Seq: r.Seq, LastMessageBoxId: r.LastMessageBoxId, Date: r.Date2}
}

const selectStateQuery = "SELECT pts, qts, seq, last_message_box_id, date2 FROM user_state WHERE user_id = ?"

// ErrInsideRun is returned by Run and Read called with the context of a Box:
// inside a box transaction, the state is b.State() and nested transactions
// are not allowed.
var ErrInsideRun = errors.New("userbox: called inside a box transaction")

// Open returns the connection pool for dsn that the service's DAOs use as
// well: go-zero keeps one *sql.DB per DSN, so Run shares the pool (and its
// size) with the rest of the service.
func Open(dsn string) (*sql.DB, error) {
	return sqlx.NewMysql(dsn).RawDB()
}

// Read returns the committed state of userId's box: the state a client may
// be given (updates.getState). Every journal row up to its pts is committed
// and visible. A user without a row is in the base state.
func Read(ctx context.Context, db *sql.DB, userId int64) (State, error) {
	if insideRun(ctx) {
		return State{}, ErrInsideRun
	}
	var s State
	err := db.QueryRowContext(ctx, selectStateQuery, userId).Scan(&s.Pts, &s.Qts, &s.Seq, &s.LastMessageBoxId, &s.Date)
	if errors.Is(err, sql.ErrNoRows) {
		return baseState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("userbox: read state of user %d: %w", userId, err)
	}
	return s, nil
}
