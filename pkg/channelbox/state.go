package channelbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Read returns the committed state of channelId's box: the state a client
// may be given. Every journal row up to its pts is committed and visible.
// No row is ErrNoChannel.
func Read(ctx context.Context, db *sql.DB, channelId int64) (State, error) {
	var s State
	err := db.QueryRowContext(ctx, "SELECT pts, last_message_id, date2 FROM channels WHERE id = ?", channelId).
		Scan(&s.Pts, &s.LastMessageId, &s.Date)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, ErrNoChannel
	}
	if err != nil {
		return State{}, fmt.Errorf("channelbox: read state of channel %d: %w", channelId, err)
	}
	return s, nil
}

// JournalReport is what CheckJournal found in a channel's journal.
type JournalReport struct {
	State    State           // the committed state, read first
	Rows     int             // journal rows with pts in (after, State.Pts]
	Gaps     []userbox.Range // pts no row covers
	Overlaps []userbox.Range // pts covered by more than one row
}

// OK tells whether the journal covers the range exactly once.
func (r JournalReport) OK() bool {
	return len(r.Gaps) == 0 && len(r.Overlaps) == 0
}

// CheckJournal checks that channelId's journal rows cover (after,
// state.pts] exactly once, as userbox.CheckJournal does for a user: the
// committed state first, then the rows up to its pts.
func CheckJournal(ctx context.Context, db *sql.DB, channelId int64, after int32) (JournalReport, error) {
	st, err := Read(ctx, db, channelId)
	if err != nil {
		return JournalReport{}, err
	}
	rep := JournalReport{State: st}
	if st.Pts <= after {
		return rep, nil
	}
	rows, err := db.QueryContext(ctx,
		"SELECT pts, pts_count FROM channel_pts_updates WHERE channel_id = ? AND pts > ? AND pts <= ? ORDER BY pts",
		channelId, after, st.Pts)
	if err != nil {
		return JournalReport{}, fmt.Errorf("channelbox: read journal of channel %d: %w", channelId, err)
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
			rep.Gaps = append(rep.Gaps, userbox.Range{From: next, To: start})
		case start < next:
			rep.Overlaps = append(rep.Overlaps, userbox.Range{From: start, To: min(next, pts)})
		}
		next = max(next, pts)
	}
	if err = rows.Err(); err != nil {
		return JournalReport{}, err
	}
	if next < st.Pts {
		rep.Gaps = append(rep.Gaps, userbox.Range{From: next, To: st.Pts})
	}
	return rep, nil
}
