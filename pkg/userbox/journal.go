package userbox

import (
	"context"
	"database/sql"
	"fmt"
)

// Range is the pts range (From, To].
type Range struct {
	From, To int32
}

// JournalReport is what CheckJournal found in a user's journal.
type JournalReport struct {
	State    State   // the committed state, read first
	Rows     int     // journal rows with pts in (after, State.Pts]
	Gaps     []Range // pts no row covers
	Overlaps []Range // pts covered by more than one row
}

// OK tells whether the journal covers the range exactly once.
func (r JournalReport) OK() bool {
	return len(r.Gaps) == 0 && len(r.Overlaps) == 0
}

// CheckJournal checks that userId's journal rows cover (after, state.pts]
// exactly once: each row covers (pts - pts_count, pts]. It reads the
// committed state first and then the rows up to its pts, as
// updates.getDifference does, so rows committed in between are not looked
// at. after must be the base pts or a pts some row ends at.
func CheckJournal(ctx context.Context, db *sql.DB, userId int64, after int32) (JournalReport, error) {
	st, err := Read(ctx, db, userId)
	if err != nil {
		return JournalReport{}, err
	}
	rep := JournalReport{State: st}
	if st.Pts <= after {
		return rep, nil
	}

	rows, err := db.QueryContext(ctx,
		"SELECT pts, pts_count FROM user_pts_updates WHERE user_id = ? AND pts > ? AND pts <= ? ORDER BY pts",
		userId, after, st.Pts)
	if err != nil {
		return JournalReport{}, fmt.Errorf("userbox: read journal of user %d: %w", userId, err)
	}
	defer rows.Close()

	next := after // covered up to here
	for rows.Next() {
		var pts, count int32
		if err = rows.Scan(&pts, &count); err != nil {
			return JournalReport{}, err
		}
		rep.Rows++
		start := pts - count
		switch {
		case start > next:
			rep.Gaps = append(rep.Gaps, Range{From: next, To: start})
		case start < next:
			rep.Overlaps = append(rep.Overlaps, Range{From: start, To: min(next, pts)})
		}
		next = max(next, pts)
	}
	if err = rows.Err(); err != nil {
		return JournalReport{}, err
	}
	if next < st.Pts {
		rep.Gaps = append(rep.Gaps, Range{From: next, To: st.Pts})
	}
	return rep, nil
}
