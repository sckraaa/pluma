// Package outbox delivers the events of box transactions (ADR 0012).
//
// An event is a row of mq_outbox written by the transaction whose change it
// describes (userbox.Box.Emit), or alone by Insert when no box changes. The
// Relay is the only Kafka producer of the system: it publishes the rows in
// the order of their ids and then
//
//   - deletes a push row (topic class push: Sync-T, BotSync) once the broker
//     has it: a push lost later is caught up by updates.getDifference;
//   - marks an inbox row published (Inbox-T): the recipient's box
//     transaction applies the event from the row and deletes it
//     (userbox.Box.TakeEvent), so the row is the event until applied, and
//     the copy in Kafka only says "the row is ready".
//
// One relay of a database publishes at a time (a MySQL named lock); the
// others wait. Nothing depends on Kafka to start: the relay connects in the
// background, and rows wait in the table while the broker is away.
//
// The classes of the topics come from the service config (Register).
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Partitions every topic of the relay must have (topics.sh creates them):
// the key, the recipient, keeps the order of a recipient in one partition;
// the count is the parallelism and the isolation of the consumers.
const Partitions = 16

// Topics are the topics of a service and their classes.
type Topics struct {
	Inbox []string // applied by box transactions (TopicInbox)
	Push  []string // pushes to devices (TopicPush)
}

// Register declares the topics of the process (userbox.RegisterTopic): an
// event of a topic not registered is refused, and the relay publishes the
// registered ones.
func Register(t Topics) error {
	for _, topic := range t.Inbox {
		if err := userbox.RegisterTopic(topic, userbox.TopicInbox); err != nil {
			return err
		}
	}
	for _, topic := range t.Push {
		if err := userbox.RegisterTopic(topic, userbox.TopicPush); err != nil {
			return err
		}
	}
	return nil
}

const insertQuery = "INSERT INTO mq_outbox (initiator_id, topic, recipient_id, method, payload, trace, created_at) VALUES (0, ?, ?, ?, ?, ?, ?)"

// Insert writes an event outside a box transaction, as one autocommit
// INSERT (initiator 0): a push that changes no box, an inbox event of an
// operation that writes no box of its initiator. Once Insert returns, the
// event is durable and will be delivered; an error means it is not
// written. It wakes the relays of the process.
func Insert(ctx context.Context, db *sql.DB, ev userbox.Event) (int64, error) {
	if err := userbox.CheckEvent(ev, 0); err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, insertQuery, ev.Topic, ev.Recipient, ev.Method, ev.Data, userbox.TraceParent(ctx), time.Now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("outbox: insert %s to user %d: %w", ev.Method, ev.Recipient, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	Notify()
	return id, nil
}

var waiters = struct {
	sync.Mutex
	m map[chan struct{}]struct{}
}{m: map[chan struct{}]struct{}{}}

// Notify wakes the relays of the process: rows were committed. A relay
// that is busy runs another round right after the current one; the relays
// of other processes find the rows by polling.
func Notify() {
	waiters.Lock()
	defer waiters.Unlock()
	for ch := range waiters.m {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	waiters.Lock()
	waiters.m[ch] = struct{}{}
	waiters.Unlock()
	return ch, func() {
		waiters.Lock()
		delete(waiters.m, ch)
		waiters.Unlock()
	}
}

// lockName is the name of the relay's MySQL lock for the current database.
// GET_LOCK names are global to the server: the database in the name lets
// two databases of one server have a relay each.
const lockName = "CONCAT('mq_outbox_relay.', DATABASE())"

func inList(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// Stored is an event as its row holds it.
type Stored struct {
	Id        int64
	Initiator int64
	userbox.Event
	Trace     string
	CreatedAt int64 // unix ms
}

// Load reads the event id from its row: what a consumer applies (the copy
// in Kafka of an inbox event has no body). nil: there is no row, the event
// is applied or discarded, for good, since ids are never reused.
func Load(ctx context.Context, db *sql.DB, id int64) (*Stored, error) {
	ev := &Stored{Id: id}
	err := db.QueryRowContext(ctx, "SELECT initiator_id, topic, recipient_id, method, payload, trace, created_at FROM mq_outbox WHERE id = ?", id).
		Scan(&ev.Initiator, &ev.Topic, &ev.Recipient, &ev.Method, &ev.Data, &ev.Trace, &ev.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ev, nil
}
