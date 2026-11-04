// Package channelbox keeps the box of each channel and supergroup (ADR
// 0034): its pts and message ids in the channel's row of channels, its
// journal in channel_pts_updates, and changes them only inside the MySQL
// transaction that writes what they describe.
//
// A channel is changed by a user's action, in that user's box transaction
// (pkg/userbox): the channel's row is locked after the user's user_state
// row, and the user's dialog, seq rows and events commit together with the
// channel's rows:
//
//	res, err := userbox.Run(ctx, db, userId, func(b *userbox.Box) error {
//		c, err := channelbox.Open(b, channelId)
//		if err != nil {
//			return err // ErrNoChannel: no such channel
//		}
//		id := c.NextMessageId()
//		// the message row of channel_messages, through b.Tx()
//		c.AddPtsUpdate(update, 1) // journal row, the channel's pts += 1
//		b.Emit(event)             // mq_outbox row, same transaction
//		return nil
//	})
//	for _, r := range channelbox.Results(res) { ... }
//
// Create inserts a new channel's row instead of Open. Allocation happens
// in memory under the row lock and is written by the user's Run before its
// events (userbox.Part), with a conditional UPDATE of the counters; a
// rollback burns nothing. Every pts of a channel a client sees is committed
// with its journal row (K1), the journal has no gaps or overlaps (K2),
// commits of a channel happen in pts order (K3), and pts and message ids
// never go down (K4).
//
// Lock order (ADR 0034, p. 2): the user's user_state, then the channel rows
// in ascending id, then everything else. Open and Create refuse a channel
// whose id is below one already locked by the transaction. A transaction
// that writes a dialogs row together with a channel opens the channel
// first.
//
// Only this package writes channels.pts, channels.last_message_id and
// channel_pts_updates (tools/boxaudit). The other columns of channels
// (title, counts, top_message, ...) are data of the transaction and are
// written by the callers through b.Tx().
//
// A system action without a user's box (none yet) would need a Run of its
// own; it is added with the first such action.
package channelbox
