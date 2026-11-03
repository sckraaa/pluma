// Package channelbox may write the counters and the journal of a channel.
package channelbox

const (
	journal = "INSERT INTO channel_pts_updates (channel_id, pts) VALUES (?, ?)"
	row     = "UPDATE channels SET pts = ?, last_message_id = ? WHERE id = ?"
)
