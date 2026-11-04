package userbox

import (
	"context"
	"database/sql"
)

// Part is another box that a user's transaction locks and changes together
// with the user's own: a channel (pkg/channelbox, ADR 0034, p. 2). Its rows
// lock after user_state, and Run writes them with the transaction, after
// the user's journal and counters and before the events.
type Part interface {
	// Flush writes the part's journal rows and counters through the
	// Box's Tx. An error rolls the transaction back.
	Flush(ctx context.Context) error
	// Journaled tells whether the part wrote journal rows: a COMMIT of
	// unknown outcome can then be checked by them.
	Journaled() bool
	// Applied tells whether the part's rows of a COMMIT that failed are
	// there. It runs on tx, the transaction of the check on another
	// connection, after the user's user_state row is locked: the
	// transaction it checks held that lock to its end, so it is over, and
	// the part reads its rows without locking them.
	Applied(ctx context.Context, tx *sql.Tx) (bool, error)
}

// Attach adds p to the transaction. The Part's own rules (lock order, what
// it may write) are its package's.
func (b *Box) Attach(p Part) {
	if b.err != nil {
		return
	}
	if p == nil {
		b.err = misuse("user %d: nil part", b.userId)
		return
	}
	b.parts = append(b.parts, p)
}

// Parts are the parts attached so far, in the order of Attach.
func (b *Box) Parts() []Part { return b.parts }

// Fail rolls the transaction back with err: a misuse found by a Part.
// The first failure is kept.
func (b *Box) Fail(err error) {
	if b.err == nil && err != nil {
		b.err = err
	}
}

// Err is the misuse that will roll the transaction back, nil without one.
func (b *Box) Err() error { return b.err }

// flushParts writes the parts' rows in the order of Attach.
func flushParts(ctx context.Context, b *Box) error {
	for _, p := range b.parts {
		if err := p.Flush(ctx); err != nil {
			return err
		}
		if err := b.rec.failed(); err != nil {
			return err
		}
	}
	return nil
}

// partsJournaled tells whether some part wrote journal rows.
func (b *Box) partsJournaled() bool {
	for _, p := range b.parts {
		if p.Journaled() {
			return true
		}
	}
	return false
}
