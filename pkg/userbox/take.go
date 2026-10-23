package userbox

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"google.golang.org/grpc/status"
)

// Applying an inbox event (ADR 0012, p. 5). The recipient's box transaction
// takes the event's mq_outbox row, applies the event and deletes the row:
// the row is the proof that the event is not applied yet, so a copy
// delivered again finds nothing and changes nothing. An event applies only
// once every earlier visible row of its recipient on its topic (its lane)
// is applied.

// EventState is the state column of an mq_outbox row.
type EventState int8

const (
	// EventPending: to publish, in the order of ids.
	EventPending EventState = 0
	// EventPublished: an inbox event the broker has; the recipient's
	// transaction deletes it.
	EventPublished EventState = 1
	// EventDead: an inbox event that did not apply, or blocked its lane.
	// Its recipient's later events park behind it until the lane is
	// replayed.
	EventDead EventState = 2
	// EventParked: a later event of a lane with a dead one.
	EventParked EventState = 3
)

func (s EventState) String() string {
	switch s {
	case EventPending:
		return "pending"
	case EventPublished:
		return "published"
	case EventDead:
		return "dead"
	case EventParked:
		return "parked"
	}
	return fmt.Sprintf("state(%d)", int8(s))
}

// ParseEventState parses the name of a state.
func ParseEventState(s string) (EventState, error) {
	for _, st := range []EventState{EventPending, EventPublished, EventDead, EventParked} {
		if st.String() == s {
			return st, nil
		}
	}
	return 0, fmt.Errorf("userbox: unknown event state %q", s)
}

// TakeOutcome is what TakeEvent found.
type TakeOutcome uint8

const (
	// TakeApply: the event is next in its lane; apply it and call
	// FinishEvent (or KeepEvent for a portion that is not the last).
	TakeApply TakeOutcome = iota + 1
	// TakeDuplicate: the row is gone, the event is applied.
	TakeDuplicate
	// TakeWaiting: the row is dead or parked and waits for the replay of
	// its lane.
	TakeWaiting
	// TakeDeferred: an earlier event of the lane is not applied yet; the
	// lane goes back to pending from that event on, to be published again
	// in order.
	TakeDeferred
	// TakeParked: an earlier event of the lane is dead or parked, or the
	// event was deferred MaxDeferrals times: the event is parked.
	TakeParked
)

func (o TakeOutcome) String() string {
	switch o {
	case TakeApply:
		return "applied"
	case TakeDuplicate:
		return "duplicate"
	case TakeWaiting:
		return "waiting"
	case TakeDeferred:
		return "deferred"
	case TakeParked:
		return "parked"
	}
	return fmt.Sprintf("outcome(%d)", uint8(o))
}

const (
	// MaxDeferrals of one event: then the earliest unapplied event of the
	// lane goes dead (lane_blocked) and the event parks, instead of
	// deferring the lane once more. A deferral settles a race in one round;
	// deferring again for the same row is a fault, not a race.
	MaxDeferrals = 10
	// DeadDelay is when a dead event is replayed automatically the first
	// time; each replay doubles it, up to MaxDeadDelay.
	DeadDelay    = 15 * time.Minute
	MaxDeadDelay = 4 * time.Hour

	// errorBytes is the size of the error column.
	errorBytes = 512
)

// ReplayDelay is how long after it went dead an event replayed replays
// times before is replayed again automatically.
func ReplayDelay(replays int) time.Duration {
	d := DeadDelay
	for range replays {
		if d >= MaxDeadDelay {
			break
		}
		d *= 2
	}
	return min(d, MaxDeadDelay)
}

var (
	// ErrMisuse marks a wrong use of a Box: the transaction is rolled back
	// and the same input fails the same way again.
	ErrMisuse = errors.New("userbox: misuse of a box")
	// ErrEventNotFinished: TakeEvent said apply, fn returned nil and
	// neither FinishEvent nor KeepEvent was called.
	ErrEventNotFinished = fmt.Errorf("%w: the event taken was neither finished nor kept", ErrMisuse)
	// errEventGone: the row of the event taken disappeared before
	// FinishEvent, which nothing but a box transaction of the recipient
	// may do. The attempt is rolled back; the next one finds no row.
	errEventGone = errors.New("userbox: the row of the event taken is gone")
)

func misuse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrMisuse}, args...)...)
}

// takenEvent is the event a transaction took.
type takenEvent struct {
	id      int64
	outcome TakeOutcome
	replays int
	done    bool
}

type takeRow struct {
	Topic       string `db:"topic"`
	RecipientId int64  `db:"recipient_id"`
	State       int8   `db:"state"`
	Deferrals   int    `db:"deferrals"`
	Replays     int    `db:"replays"`
}

type laneRow struct {
	Id      int64 `db:"id"`
	State   int8  `db:"state"`
	Replays int   `db:"replays"`
}

// TakeEvent takes the inbox event id addressed to the box owner, under the
// owner's lock, and decides about it (TakeOutcome). Deferring and parking
// are written by the transaction itself, so the decision and its record
// commit together. One event per transaction.
func (b *Box) TakeEvent(id int64) (TakeOutcome, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.taken != nil {
		b.err = misuse("user %d: a second event %d in one transaction", b.userId, id)
		return 0, b.err
	}
	ctx := b.ctx
	var row takeRow
	err := b.rec.QueryRowPartialCtx(ctx, &row, "SELECT topic, recipient_id, state, deferrals, replays FROM mq_outbox WHERE id = ?", id)
	switch {
	case isNoRows(err):
		b.taken = &takenEvent{id: id, outcome: TakeDuplicate}
		return TakeDuplicate, nil
	case err != nil:
		return 0, err
	}
	if row.RecipientId != b.userId {
		b.err = misuse("event %d is addressed to user %d, not %d", id, row.RecipientId, b.userId)
		return 0, b.err
	}
	if class, _ := TopicClassOf(row.Topic); class != TopicInbox {
		b.err = misuse("event %d of topic %q is not an inbox event", id, row.Topic)
		return 0, b.err
	}
	taken := &takenEvent{id: id, replays: row.Replays}
	b.taken = taken
	if s := EventState(row.State); s == EventDead || s == EventParked {
		taken.outcome = TakeWaiting
		return TakeWaiting, nil
	}

	// The earliest visible row of the lane before this one. A row with a
	// smaller id that is not committed yet is not seen and not waited for:
	// nothing that event's initiator did can depend on it. The lane is read
	// by its index whatever the statistics say: a scan of the primary key
	// below id would read every unapplied row of every recipient.
	var earlier laneRow
	err = b.rec.QueryRowPartialCtx(ctx, &earlier,
		"SELECT id, state, replays FROM mq_outbox FORCE INDEX (recipient) WHERE topic = ? AND recipient_id = ? AND id < ? ORDER BY id LIMIT 1",
		row.Topic, b.userId, id)
	switch {
	case isNoRows(err):
		taken.outcome = TakeApply
		return TakeApply, nil
	case err != nil:
		return 0, err
	}

	now := time.Now()
	switch s := EventState(earlier.State); {
	case s == EventDead || s == EventParked:
		taken.outcome = TakeParked
		return TakeParked, b.parkEvent(id)
	case row.Deferrals >= MaxDeferrals:
		// The same earlier row keeps the event waiting: it goes dead, its
		// lane parks behind it until replayed.
		if _, err = b.rec.ExecCtx(ctx,
			"UPDATE mq_outbox SET state = ?, revision = revision + 1, error = ?, due_at = ? WHERE id = ?",
			EventDead, "lane_blocked", now.Add(ReplayDelay(earlier.Replays)).UnixMilli(), earlier.Id); err != nil {
			return 0, err
		}
		taken.outcome = TakeParked
		return TakeParked, b.parkEvent(id)
	}

	// Defer: the published earlier rows of the lane and this one go back
	// to pending and are published again from the earliest on. The
	// revision moves even for a row the relay has not marked published
	// yet, so the relay's late answer does not mark it.
	var lane []laneRow
	if err = b.rec.QueryRowsPartialCtx(ctx, &lane,
		"SELECT id, state, replays FROM mq_outbox FORCE INDEX (recipient) WHERE topic = ? AND recipient_id = ? AND id < ?",
		row.Topic, b.userId, id); err != nil {
		return 0, err
	}
	var ids []int64
	for _, l := range lane {
		if EventState(l.State) == EventPublished {
			ids = append(ids, l.Id)
		}
	}
	ids = append(ids, id)
	args := make([]any, 0, len(ids)+2)
	args = append(args, EventPending, id)
	for _, i := range ids {
		args = append(args, i)
	}
	if _, err = b.rec.ExecCtx(ctx,
		"UPDATE mq_outbox SET state = ?, revision = revision + 1, deferrals = deferrals + (id = ?) WHERE id IN ("+placeholders(len(ids))+")",
		args...); err != nil {
		return 0, err
	}
	taken.outcome = TakeDeferred
	return TakeDeferred, nil
}

func (b *Box) parkEvent(id int64) error {
	_, err := b.rec.ExecCtx(b.ctx, "UPDATE mq_outbox SET state = ?, revision = revision + 1 WHERE id = ?", EventParked, id)
	return err
}

// applying returns the event taken for applying, or records a misuse.
func (b *Box) applying(what string) *takenEvent {
	if b.err != nil {
		return nil
	}
	t := b.taken
	if t == nil || t.outcome != TakeApply || t.done {
		b.err = misuse("user %d: %s without an event to apply", b.userId, what)
		return nil
	}
	return t
}

// FinishEvent deletes the row of the event taken: the event is applied
// when the transaction commits.
func (b *Box) FinishEvent() error {
	t := b.applying("FinishEvent")
	if t == nil {
		return b.err
	}
	res, err := b.rec.ExecCtx(b.ctx, "DELETE FROM mq_outbox WHERE id = ?", t.id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if err == nil {
			err = errEventGone
		}
		b.err = err
		return err
	}
	t.done = true
	return nil
}

// KeepEvent keeps the row of the event taken: the transaction applied a
// portion of it, and the next transaction takes it again for the next one.
func (b *Box) KeepEvent() {
	if t := b.applying("KeepEvent"); t != nil {
		t.done = true
	}
}

// FailEvent makes the event taken dead: it did not apply after attempts
// tries with cause, a deterministic error. Its recipient's later events
// park behind it; the relay replays the lane after ReplayDelay.
func (b *Box) FailEvent(cause error, attempts int) error {
	t := b.applying("FailEvent")
	if t == nil {
		return b.err
	}
	if _, err := b.rec.ExecCtx(b.ctx,
		"UPDATE mq_outbox SET state = ?, revision = revision + 1, attempts = attempts + ?, error = ?, due_at = ? WHERE id = ?",
		EventDead, attempts, EventError(cause), time.Now().Add(ReplayDelay(t.replays)).UnixMilli(), t.id); err != nil {
		return err
	}
	t.done = true
	return nil
}

func (b *Box) takenOutcome() TakeOutcome {
	if b.taken == nil {
		return 0
	}
	return b.taken.outcome
}

// unfinished tells whether the transaction took an event to apply and
// left it neither finished nor kept.
func (b *Box) unfinished() bool {
	return b.taken != nil && b.taken.outcome == TakeApply && !b.taken.done
}

// EventError is the text of err for the error column of mq_outbox: codes
// and ids, never content (a duplicate key message of MySQL quotes the
// row's values).
func EventError(err error) string {
	var s string
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		s = fmt.Sprintf("mysql %d", me.Number)
	} else if st, ok := status.FromError(err); ok && st.Code() >= minMTProtoCode {
		s = fmt.Sprintf("rpc %d %s", st.Code(), st.Message())
	} else {
		s = err.Error()
	}
	if len(s) > errorBytes {
		s = strings.ToValidUTF8(s[:errorBytes], "")
	}
	return s
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
