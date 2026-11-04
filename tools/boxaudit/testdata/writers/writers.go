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

// The channel box: its journal, a new channel row and its counters only in
// pkg/channelbox; the other columns of channels are free.
var (
	channelJournal = "INSERT INTO channel_pts_updates (channel_id, pts) VALUES (?, ?)"
	channelRow     = "INSERT INTO channels (id, title) VALUES (?, ?)"
	channelPts     = "UPDATE channels SET title = ?, `pts` = ? WHERE id = ?"
	channelIds     = "update channels set last_message_id = ? where id = ?"
	channelAlias   = "UPDATE IGNORE channels c SET c.pts = ? WHERE c.id = ?"
	channelNoInto  = "INSERT channels (id) VALUES (?)"
	channelPrune   = "DELETE j FROM channel_pts_updates j WHERE j.channel_id = ?"
	channelTitle   = "UPDATE channels SET title = ?, top_message = ? WHERE id = ? AND pts = ?"
	channelOther   = "INSERT INTO channels_archive (id) VALUES (?)"
	channelMessage = "INSERT INTO channel_messages (channel_id, id, pts) VALUES (?, ?, ?)"
	channelCount   = "UPDATE channels SET kicked_pts = ?, participants_count = ? WHERE id = ?"
)
