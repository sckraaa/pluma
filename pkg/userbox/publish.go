package userbox

import (
	"context"
	"errors"
	"time"
)

const (
	// EventTimeout limits the publication of one event after commit.
	EventTimeout = 10 * time.Second
	// PublishTimeout limits the publication of all the events of one
	// transaction: Publish takes no longer, and what is not sent by then is
	// not sent. A lock held around a transaction and its publication must
	// outlive MaxRunTime and PublishTimeout together.
	PublishTimeout = 2 * EventTimeout
)

var (
	// ErrSkipped marks an event not published because an earlier event to
	// the same recipient failed.
	ErrSkipped = errors.New("userbox: an earlier event to the recipient failed")
	// ErrTopicTimedOut marks an event not published because an earlier
	// event of the same topic ran out of EventTimeout: the broker does not
	// answer, and every later event of the topic would wait as long.
	ErrTopicTimedOut = errors.New("userbox: an earlier event of the topic timed out")
	// ErrPublishTimeout marks an event not published because the events of
	// the transaction took PublishTimeout.
	ErrPublishTimeout = errors.New("userbox: the time to publish the transaction's events is over")
)

// Failure is an event of a committed transaction that was not published.
type Failure struct {
	Event Event
	Err   error // the error of send, or ErrSkipped, ErrTopicTimedOut, ErrPublishTimeout
}

// Publish sends the events of a committed transaction with send, one after
// another in their order, in a context that the request's cancellation does
// not reach: the data is committed, the side effects must follow. Each send
// gets EventTimeout, all of them together PublishTimeout. When an event to a
// recipient fails, the later events to that recipient are not sent (it
// would get them without the one before); events to other recipients still
// are, unless the failure was a timeout: then the later events of the topic
// are not sent either. It returns what was not sent; the operation itself
// stays successful.
func Publish(ctx context.Context, events []Event, send func(context.Context, Event) error) []Failure {
	return publish(ctx, events, send, EventTimeout, PublishTimeout)
}

func publish(ctx context.Context, events []Event, send func(context.Context, Event) error, eventTimeout, publishTimeout time.Duration) []Failure {
	var (
		failures []Failure
		failed   map[int64]bool
		down     map[string]bool
	)
	base, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	fail := func(ev Event, err error) {
		if failed == nil {
			failed = map[int64]bool{}
		}
		failed[ev.Recipient] = true
		failures = append(failures, Failure{Event: ev, Err: err})
	}
	for _, ev := range events {
		switch {
		case failed[ev.Recipient]:
			fail(ev, ErrSkipped)
			continue
		case down[ev.Topic]:
			fail(ev, ErrTopicTimedOut)
			continue
		case base.Err() != nil:
			fail(ev, ErrPublishTimeout)
			continue
		}
		ectx, ecancel := context.WithTimeout(base, eventTimeout)
		err := send(ectx, ev)
		timedOut := ectx.Err() != nil || errors.Is(err, context.DeadlineExceeded)
		ecancel()
		if err == nil {
			continue
		}
		if timedOut {
			if down == nil {
				down = map[string]bool{}
			}
			down[ev.Topic] = true
		}
		fail(ev, err)
	}
	return failures
}
