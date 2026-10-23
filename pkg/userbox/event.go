package userbox

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/propagation"
)

// The side effects of a box transaction are rows of the table mq_outbox,
// written by that transaction (ADR 0012): an event exists exactly when the
// change it describes is committed. The relay of pkg/outbox publishes the
// rows to Kafka in the order of their ids.

// Event is a side effect of a box transaction: a push to sync or an event
// for another user's inbox, one mq_outbox row.
type Event struct {
	Topic     string // Kafka topic, registered with RegisterTopic
	Recipient int64  // the user the event is addressed to; the Kafka key is its decimal form
	Method    string // the "method" header: the request's proto message name
	Data      []byte // JSON of the request
}

// Limits of events (ADR 0012, p. 2).
const (
	// MaxEventBytes is the largest payload of an event.
	MaxEventBytes = 4 << 20
	// MinMaxAllowedPacket is the smallest max_allowed_packet of MySQL that
	// takes the statements writing events (Check).
	MinMaxAllowedPacket = 16 << 20
	// maxMethodBytes is the size of the method column.
	maxMethodBytes = 128
	// Bounds of one INSERT of events: the payloads together, the rows.
	maxStatementPayload = 4 << 20
	maxStatementRows    = 500
)

// TopicClass is how the events of a topic are delivered. It is a property
// of the topic, never of an event: a caller cannot turn an inbox event into
// a push whose row the relay deletes before it is applied.
type TopicClass uint8

const (
	// TopicPush: delivery is not transactional, the relay deletes the row
	// once the broker has it. A push lost later is caught up by
	// updates.getDifference.
	TopicPush TopicClass = iota + 1
	// TopicInbox: the recipient's box transaction applies the event and
	// deletes its row (Box.TakeEvent).
	TopicInbox
)

func (c TopicClass) String() string {
	switch c {
	case TopicPush:
		return "push"
	case TopicInbox:
		return "inbox"
	}
	return fmt.Sprintf("class(%d)", uint8(c))
}

var topics = struct {
	sync.RWMutex
	m map[string]TopicClass
}{m: map[string]TopicClass{}}

// RegisterTopic declares a topic of the process and its class, at start,
// from the service config (pkg/outbox). Registering a topic again with the
// same class does nothing; with another class it fails.
func RegisterTopic(topic string, class TopicClass) error {
	if topic == "" || class != TopicPush && class != TopicInbox {
		return fmt.Errorf("userbox: register topic %q of %v", topic, class)
	}
	topics.Lock()
	defer topics.Unlock()
	if c, ok := topics.m[topic]; ok && c != class {
		return fmt.Errorf("userbox: topic %q is %v, not %v", topic, c, class)
	}
	topics.m[topic] = class
	return nil
}

// TopicClassOf returns the class of a registered topic.
func TopicClassOf(topic string) (TopicClass, bool) {
	topics.RLock()
	defer topics.RUnlock()
	c, ok := topics.m[topic]
	return c, ok
}

// Topics returns the registered topics with their classes.
func Topics() map[string]TopicClass {
	topics.RLock()
	defer topics.RUnlock()
	return maps.Clone(topics.m)
}

// ErrInvalidEvent marks an event that cannot be written: an unknown topic,
// no recipient, a payload over MaxEventBytes, an inbox event to its
// initiator. Nothing is written; the transaction that emitted it rolls
// back.
var ErrInvalidEvent = errors.New("userbox: invalid event")

// CheckEvent tells whether initiator may write ev: initiator is the owner
// of the box transaction, 0 outside one.
func CheckEvent(ev Event, initiator int64) error {
	class, ok := TopicClassOf(ev.Topic)
	switch {
	case !ok:
		return fmt.Errorf("%w: topic %q is not registered", ErrInvalidEvent, ev.Topic)
	case ev.Recipient <= 0:
		return fmt.Errorf("%w: %s without a recipient", ErrInvalidEvent, ev.Method)
	case ev.Method == "" || len(ev.Method) > maxMethodBytes:
		return fmt.Errorf("%w: method of %d bytes", ErrInvalidEvent, len(ev.Method))
	case len(ev.Data) > MaxEventBytes:
		return fmt.Errorf("%w: %s to user %d of %d bytes, the limit is %d", ErrInvalidEvent, ev.Method, ev.Recipient, len(ev.Data), MaxEventBytes)
	case class == TopicInbox && ev.Recipient == initiator:
		return fmt.Errorf("%w: inbox event %s addressed to its initiator %d", ErrInvalidEvent, ev.Method, initiator)
	}
	return nil
}

// TraceParent is the W3C traceparent of the span in ctx, or "" without
// one: the trace of an event, carried to its consumer.
func TraceParent(ctx context.Context) string {
	c := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, c)
	return c["traceparent"]
}

// outboxRow is an event as the transaction writes it.
type outboxRow struct {
	Event
	trace     string
	createdAt int64 // unix ms of Emit
}

const (
	insertEvents   = "INSERT INTO mq_outbox (initiator_id, topic, recipient_id, method, payload, trace, created_at) VALUES "
	insertEventRow = "(?, ?, ?, ?, ?, ?, ?)"
)

// Emit adds an event to write with the transaction, after its other
// statements, in this order. An event CheckEvent refuses rolls the
// transaction back.
func (b *Box) Emit(ev Event) {
	if b.err != nil {
		return
	}
	if err := CheckEvent(ev, b.userId); err != nil {
		b.err = err
		return
	}
	b.events = append(b.events, outboxRow{Event: ev, trace: TraceParent(b.ctx), createdAt: time.Now().UnixMilli()})
}

// writeEvents writes the events of the transaction: multi-row INSERTs in
// the order of Emit, each with at most maxStatementPayload of payloads and
// maxStatementRows rows. The ids AUTO_INCREMENT gives them follow that
// order, and, since the transaction holds the initiator's user_state lock,
// the order of the initiator's commits.
func writeEvents(ctx context.Context, b *Box) error {
	for start := 0; start < len(b.events); {
		end, size := start, 0
		for end < len(b.events) && end-start < maxStatementRows &&
			(end == start || size+len(b.events[end].Data) <= maxStatementPayload) {
			size += len(b.events[end].Data)
			end++
		}
		args := make([]any, 0, 7*(end-start))
		for _, e := range b.events[start:end] {
			args = append(args, b.userId, e.Topic, e.Recipient, e.Method, e.Data, e.trace, e.createdAt)
		}
		query := insertEvents + strings.Repeat(insertEventRow+", ", end-start-1) + insertEventRow
		if _, err := b.rec.ExecCtx(ctx, query, args...); err != nil {
			return err
		}
		start = end
	}
	return nil
}
