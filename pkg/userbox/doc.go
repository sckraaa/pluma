// Package userbox keeps the state of each user's common message box (pts and
// message box ids; qts and seq later) in MySQL and changes it only inside the
// transaction that writes the data those counters describe (ADR 0011).
//
// The table user_state holds one row per user: the last committed pts and the
// last issued user_message_box_id. A user without a row is in the base state
// (pts 1, no box ids). Run is the only writer:
//
//	res, err := userbox.Run(ctx, db, userId, func(b *userbox.Box) error {
//		// reads and writes of this user's rows, all through b.Tx()
//		id := b.NextMessageId()
//		...
//		b.AddPtsUpdate(update, 1)          // journal row, pts += 1
//		b.Emit(event)                      // mq_outbox row, same transaction
//		return nil
//	})
//
// Run locks the user's user_state row first (creating it on the first write),
// calls fn, writes the journal rows (user_pts_updates), the new counters and
// the events (mq_outbox, ADR 0012), and commits. Allocation happens in memory under the row lock and becomes
// visible only with the commit, together with the journal and the data, so
// every pts a client sees is committed with what it describes, there are no
// burned pts, and commits of one user happen in pts order. The counters do
// not depend on Redis.
//
// Rules for fn (checked by tests on a one-connection pool):
//
//   - every statement goes through b.Tx(); a read through the pool does not
//     see the transaction's own writes and takes a second connection;
//   - no network calls (RPC, Kafka, Redis): side effects go to b.Emit,
//     whose rows the relay of pkg/outbox publishes once committed, and to
//     b.InvalidateCache, done by the caller after commit;
//   - checks that depend on the box are made on rows read inside fn;
//   - a business no-op returns nil before the first write, a business error
//     is returned as is (rolled back, not retried);
//   - no Run inside fn.
//
// After the first failed statement the transaction sends nothing more and
// Run rolls back, whatever fn returns. Deadlocks (1213), lock wait timeouts
// (1205) and commits found not applied are retried on a fresh Box. Read gives
// the committed state; Check verifies the MySQL settings Run relies on.
//
// An inbox event is applied by its recipient's transaction: b.TakeEvent
// checks that its row is there and next in its lane, b.FinishEvent deletes
// it with the change (exactly once, in order); b.FailEvent makes it dead.
package userbox
