// Package writers writes the box state and the journal outside pkg/userbox;
// the test checks the findings by line.
package writers

const (
	journal = "INSERT INTO user_pts_updates (user_id, pts) VALUES (?, ?)"
	state   = "update `user_state` set pts = ? where user_id = ?"
	read    = "SELECT pts FROM user_state WHERE user_id = ?"
	other   = "UPDATE user_states_archive SET pts = 0"
)

// A comment saying UPDATE user_state is not SQL.
var raw = `DELETE
	FROM user_pts_updates WHERE user_id = ?`

// The events: only pkg/userbox and pkg/outbox.
var events = "DELETE FROM mq_outbox WHERE id = ?"

// The seq journal: only pkg/userbox.
var seqJournal = "INSERT INTO `user_seq_updates` (user_id, seq) VALUES (?, ?)"
