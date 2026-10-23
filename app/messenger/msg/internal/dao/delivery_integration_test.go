//go:build integration

package dao_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/teamgram/proto/mtproto"
	inbox_helper "github.com/teamgram/teamgram-server/app/messenger/msg/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	mqoutbox "github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	prom "github.com/prometheus/client_golang/prometheus"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"google.golang.org/protobuf/proto"
)

// The delivery of the tests is production's (ADR 0012): the relay on a pool
// of its own publishes mq_outbox to Kafka, the inbox consumer of msg applies
// the inbox events with the Dao of the world in use, and a reader of Sync-T
// records the pushes as sync receives them, checking that each pushed pts is
// committed.

const (
	inboxGroup    = "Inbox-IT"
	inboxInstance = "it-msg"
	drainTimeout  = 2 * time.Minute
)

type delivery struct {
	relayDB *sql.DB
	relay   *mqoutbox.Relay
	client  sarama.Client // of the reader
	offsets sarama.Client // its own: a fetch of the reader holds a connection
	reader  sarama.Consumer
	pcs     []sarama.PartitionConsumer
	wg      sync.WaitGroup

	mu      sync.Mutex
	inbox   *mqconsumer.Consumer
	handler mqconsumer.Handler
	policy  mqconsumer.Policy
	pushes  map[int64][]push
	next    map[int32]int64 // next offset of Sync-T to read, by partition
	errs    []string        // what the push reader found wrong
}

func startDelivery(ctx context.Context) (*delivery, error) {
	d := &delivery{pushes: map[int64][]push{}, next: map[int32]int64{}}
	var err error
	if d.relayDB, err = sql.Open("mysql", boxDSN("")); err != nil {
		return nil, err
	}
	d.relay = mqoutbox.NewRelay(d.relayDB, mqoutbox.RelayConf{
		Brokers: itKafka.Brokers,
		Groups:  []mqoutbox.GroupConf{{Topic: "Inbox-T", Group: inboxGroup}},
	})
	d.relay.Start()
	d.startInbox(nil)

	cfg := sarama.NewConfig()
	cfg.Version = mqoutbox.KafkaVersion
	if d.client, err = sarama.NewClient(itKafka.Brokers, cfg); err != nil {
		return nil, err
	}
	if d.reader, err = sarama.NewConsumerFromClient(d.client); err != nil {
		return nil, err
	}
	if d.offsets, err = sarama.NewClient(itKafka.Brokers, cfg); err != nil {
		return nil, err
	}
	for p := range int32(mqoutbox.Partitions) {
		pc, err := d.reader.ConsumePartition("Sync-T", p, sarama.OffsetOldest)
		if err != nil {
			return nil, err
		}
		d.pcs = append(d.pcs, pc)
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for m := range pc.Messages() {
				d.record(m)
			}
		}()
	}
	return d, nil
}

// startInbox starts the inbox consumer; after runs after each message is
// done and before its offset is marked.
func (d *delivery) startInbox(after func(m *mqconsumer.Message)) {
	c := mqconsumer.New(mqconsumer.Conf{Brokers: itKafka.Brokers, Topic: "Inbox-T", Group: inboxGroup, InstanceId: inboxInstance},
		"msg", d, mqconsumer.Policy{
			Deterministic: userbox.Deterministic,
			Dead: func(ctx context.Context, m *mqconsumer.Message, cause error, attempts int) error {
				d.mu.Lock()
				dead := d.policy.Dead
				d.mu.Unlock()
				return dead(ctx, m, cause, attempts)
			},
		})
	if after != nil {
		c.SetAfterHandle(after)
	}
	c.Start()
	d.mu.Lock()
	d.inbox = c
	d.mu.Unlock()
}

// restartInbox stops the inbox consumer and starts it again, with the same
// static membership.
func (d *delivery) restartInbox(after func(m *mqconsumer.Message)) {
	d.mu.Lock()
	c := d.inbox
	d.mu.Unlock()
	c.Stop()
	d.startInbox(after)
}

func (d *delivery) stop() {
	d.mu.Lock()
	c := d.inbox
	d.mu.Unlock()
	c.Stop()
	d.relay.Stop()
	for _, pc := range d.pcs {
		pc.AsyncClose()
	}
	d.wg.Wait()
	_ = d.reader.Close()
	_ = d.client.Close()
	_ = d.offsets.Close()
	_ = d.relayDB.Close()
}

// use makes the consumer apply the events with the Dao of a world.
func (d *delivery) use(w *dao.Dao) {
	h, p := inbox_helper.Handler(w)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handler, d.policy = h, p
}

func (d *delivery) Handle(ctx context.Context, m *mqconsumer.Message) (mqconsumer.Outcome, error) {
	d.mu.Lock()
	h := d.handler
	d.mu.Unlock()
	return h.Handle(ctx, m)
}

// record keeps a push of Sync-T as sync receives it.
func (d *delivery) record(m *sarama.ConsumerMessage) {
	defer func() {
		d.mu.Lock()
		d.next[m.Partition] = m.Offset + 1
		d.mu.Unlock()
	}()
	var method string
	for _, h := range m.Headers {
		if string(h.Key) == mqoutbox.HeaderMethod {
			method = string(h.Value)
		}
	}
	recipient, _ := strconv.ParseInt(string(m.Key), 10, 64)
	var p push
	switch method {
	case "sync.TL_sync_updatesNotMe":
		var in syncpb.TLSyncUpdatesNotMe
		if err := json.Unmarshal(m.Value, &in); err != nil {
			d.fail("decode %s: %v", method, err)
			return
		}
		p = push{notMe: true, updates: in.Updates}
	case "sync.TL_sync_pushUpdates":
		var in syncpb.TLSyncPushUpdates
		if err := json.Unmarshal(m.Value, &in); err != nil {
			d.fail("decode %s: %v", method, err)
			return
		}
		p = push{updates: in.Updates}
	default:
		d.fail("unexpected push %s", method)
		return
	}
	st, err := userbox.Read(context.Background(), itCheck, recipient)
	if err != nil {
		d.fail("read the state of %d: %v", recipient, err)
		return
	}
	for _, u := range p.updates.GetUpdates() {
		if u.GetPts_INT32() > st.Pts {
			d.fail("push of %s with pts %d to user %d before commit (committed pts %d)", u.GetPredicateName(), u.GetPts_INT32(), recipient, st.Pts)
		}
	}
	d.mu.Lock()
	d.pushes[recipient] = append(d.pushes[recipient], p)
	d.mu.Unlock()
}

func (d *delivery) fail(format string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.errs = append(d.errs, fmt.Sprintf(format, args...))
}

func (d *delivery) pushesTo(userId int64) []push {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]push(nil), d.pushes[userId]...)
}

// unapplied counts the rows of events not yet applied or published.
func unapplied(t testing.TB) int {
	t.Helper()
	var n int
	if err := itCheck.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE state IN (?, ?)", userbox.EventPending, userbox.EventPublished).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// drain waits until every event written so far, and every event those
// caused, is applied or published, and every push published is read.
func (d *delivery) drain(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(drainTimeout)
	for unapplied(t) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d events not applied within %v", unapplied(t), drainTimeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for p := range int32(mqoutbox.Partitions) {
		end, err := d.offsets.GetOffset("Sync-T", p, sarama.OffsetNewest)
		if err != nil {
			t.Fatal(err)
		}
		for {
			d.mu.Lock()
			next := d.next[p]
			d.mu.Unlock()
			if next >= end {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("pushes of partition %d not read within %v", p, drainTimeout)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	d.mu.Lock()
	errs := d.errs
	d.errs = nil
	d.mu.Unlock()
	for _, e := range errs {
		t.Error(e)
	}
}

// worldDelivery is the delivery as a test of a world sees it.
type worldDelivery struct {
	*delivery
	t *testing.T
	// deadOK: the test makes events dead on purpose. Otherwise a drain
	// fails on a dead or parked event: a handler that does not finish an
	// event (ErrEventNotFinished) or a no-op branch that fails would leave
	// one.
	deadOK bool
}

func (w *worldDelivery) drain() {
	w.t.Helper()
	w.delivery.drain(w.t)
	if w.deadOK {
		return
	}
	rows, err := mqoutbox.List(context.Background(), itCheck, mqoutbox.ListFilter{State: ptr(userbox.EventDead)})
	if err != nil {
		w.t.Fatal(err)
	}
	parked, err := mqoutbox.List(context.Background(), itCheck, mqoutbox.ListFilter{State: ptr(userbox.EventParked)})
	if err != nil {
		w.t.Fatal(err)
	}
	for _, r := range append(rows, parked...) {
		w.t.Errorf("event %d %s to user %d is %s: %s", r.Id, r.Method, r.Recipient, r.State, r.Error)
	}
}

func ptr[T any](v T) *T { return &v }

// waitState waits until the row of event id is in one of states, or gone
// when none is given.
func waitState(t testing.TB, id int64, states ...userbox.EventState) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var st int8
		err := itCheck.QueryRow("SELECT state FROM mq_outbox WHERE id = ?", id).Scan(&st)
		if err == nil && slices.Contains(states, userbox.EventState(st)) || errors.Is(err, sql.ErrNoRows) && len(states) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("event %d: state %d (%v), want %v", id, st, err, states)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// discard deletes a row a test left on purpose.
func discard(t testing.TB, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, err := mqoutbox.Discard(context.Background(), itCheck, id); err != nil && err != mqoutbox.ErrNoRow {
			t.Fatal(err)
		}
	}
}

// counter returns the value of a counter of this process with labels.
func counter(t testing.TB, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
	metrics:
		for _, m := range f.GetMetric() {
			for k, v := range labels {
				found := false
				for _, l := range m.GetLabel() {
					if l.GetName() == k && l.GetValue() == v {
						found = true
					}
				}
				if !found {
					continue metrics
				}
			}
			total += m.GetCounter().GetValue()
		}
	}
	return total
}

// assigned is the number of Inbox-T partitions the instance that last
// changed has (mq_consumer_assigned_partitions).
func assigned(t testing.TB) int {
	t.Helper()
	families, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "mq_consumer_assigned_partitions" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "group" && l.GetValue() == inboxGroup {
					return int(m.GetGauge().GetValue())
				}
			}
		}
	}
	return 0
}

func inboxOutcomes(t testing.TB, outcome string) float64 {
	return counter(t, "mq_consumer_events_total", map[string]string{"topic": "Inbox-T", "outcome": outcome})
}

// sendEvent is the inbox event of a message from to to with dialog_message_id
// dmid.
func sendEvent(t testing.TB, to, from, dmid int64, text string) userbox.Event {
	t.Helper()
	data, err := json.Marshal(&inbox.TLInboxSendUserMessageToInboxV2{
		UserId: to, FromId: from, PeerType: mtproto.PEER_USER, PeerId: to,
		BoxList: []*mtproto.MessageBox{{
			UserId: from, SenderUserId: from, PeerType: mtproto.PEER_USER, PeerId: to,
			DialogMessageId: dmid,
			Message:         textMessage(from, mtproto.MakePeerUser(to), text),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return userbox.Event{Topic: "Inbox-T", Recipient: to, Method: "inbox.TL_inbox_sendUserMessageToInboxV2", Data: data}
}

func newDialogMessageId() int64 { return 1_000_000 + itIdSeq.Add(1) }

// insertEvent writes ev as a row; published rows are left to the test to
// copy to Kafka (the relay publishes pending ones only).
func insertEvent(t testing.TB, ev userbox.Event, state userbox.EventState) (id, createdAt int64) {
	t.Helper()
	createdAt = time.Now().UnixMilli()
	res, err := itCheck.Exec("INSERT INTO mq_outbox (initiator_id, topic, recipient_id, method, payload, created_at, state) VALUES (0, ?, ?, ?, ?, ?, ?)",
		ev.Topic, ev.Recipient, ev.Method, ev.Data, createdAt, state)
	if err != nil {
		t.Fatal(err)
	}
	if id, err = res.LastInsertId(); err != nil {
		t.Fatal(err)
	}
	mqoutbox.Notify()
	return id, createdAt
}

// copyEvent writes a copy of an inbox event to Inbox-T, as the relay does.
func copyEvent(t testing.TB, id, recipient, createdAt int64) {
	t.Helper()
	cfg := mqoutbox.ProducerConfig("it-copies")
	p, err := sarama.NewSyncProducer(itKafka.Brokers, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, _, err = p.SendMessage(&sarama.ProducerMessage{
		Topic: "Inbox-T",
		Key:   sarama.StringEncoder(strconv.FormatInt(recipient, 10)),
		Value: sarama.ByteEncoder{},
		Headers: []sarama.RecordHeader{
			{Key: []byte(mqoutbox.HeaderMethod), Value: []byte("inbox.TL_inbox_sendUserMessageToInboxV2")},
			{Key: []byte(mqoutbox.HeaderEventId), Value: []byte(strconv.FormatInt(id, 10))},
			{Key: []byte(mqoutbox.HeaderCreatedAt), Value: []byte(strconv.FormatInt(createdAt, 10))},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func texts(rows []messageRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Text)
	}
	return out
}

// An event published five times (repeated by the relay, the timer, a new
// leader) lands once, with one pts.
func TestEventDeliveredFiveTimes(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	before := inboxOutcomes(t, "duplicate")
	id, createdAt := insertEvent(t, sendEvent(t, b, a, newDialogMessageId(), "once"), userbox.EventPending)
	for range 4 {
		copyEvent(t, id, b, createdAt)
	}
	w.pub.drain()
	eventually(t, 10*time.Second, "copies handled", func() bool { return inboxOutcomes(t, "duplicate")-before >= 4 })
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"once"}) {
		t.Errorf("B's messages %v", got)
	}
	if st := w.state(b); st.Pts != userbox.BasePts+1 {
		t.Errorf("B's pts %d", st.Pts)
	}
	w.checkBox(b)
}

// Rows 10 and 11 of one recipient published in the reverse order: 11 waits
// for 10, which is applied first.
func TestEventsInLaneOrder(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	before := inboxOutcomes(t, "deferred")
	first, firstAt := insertEvent(t, sendEvent(t, b, a, newDialogMessageId(), "first"), userbox.EventPublished)
	second, secondAt := insertEvent(t, sendEvent(t, b, a, newDialogMessageId(), "second"), userbox.EventPublished)
	copyEvent(t, second, b, secondAt)
	eventually(t, 10*time.Second, "deferred", func() bool { return inboxOutcomes(t, "deferred")-before >= 1 })
	copyEvent(t, first, b, firstAt)
	w.pub.drain()
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"first", "second"}) {
		t.Errorf("B's messages %v, want first, second", got)
	}
	w.checkBox(b)
}

// An edit of a message the recipient has no copy of finds nothing to do:
// its row goes, and the next event of the recipient applies.
func TestEditWithoutCopy(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	edit, err := json.Marshal(&inbox.TLInboxEditMessageToInboxV2{
		UserId: b, FromId: a, PeerType: mtproto.PEER_USER, PeerId: b,
		NewMessage: &mtproto.MessageBox{DialogMessageId: newDialogMessageId(), Message: textMessage(a, mtproto.MakePeerUser(b), "edited")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mqoutbox.Insert(ctx, w.d.BoxDB, userbox.Event{Topic: "Inbox-T", Recipient: b, Method: "inbox.TL_inbox_editMessageToInboxV2", Data: edit}); err != nil {
		t.Fatal(err)
	}
	if _, err = mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, b, a, newDialogMessageId(), "after")); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"after"}) {
		t.Errorf("B's messages %v", got)
	}
	if n := countRowsOf(t, b); n != 0 {
		t.Errorf("%d rows of B left", n)
	}
}

func countRowsOf(t testing.TB, recipient int64) int {
	t.Helper()
	var n int
	if err := itCheck.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE topic = 'Inbox-T' AND recipient_id = ?", recipient).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func eventually(t testing.TB, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %v", what, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An event that fails the same way every time (a copy written twice hits
// the unique key) is dead after three attempts; the later events of its
// recipient park, the others' apply. Once the cause is fixed, a new relay
// leader replays the lane and everything applies in order; so does the
// operator's replay, and the operator may discard an event.
func TestDeadEventParksItsLane(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	w.pub.deadOK = true
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	dup := newDialogMessageId()
	if _, err := mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, b, a, dup, "original")); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	deadBefore := inboxOutcomes(t, "dead")
	dead, err := mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, b, a, dup, "copy written twice"))
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, dead, userbox.EventDead)
	parked, err := mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, b, a, newDialogMessageId(), "later"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, c, a, newDialogMessageId(), "to someone else")); err != nil {
		t.Fatal(err)
	}
	waitState(t, parked, userbox.EventParked)
	w.pub.drain()
	if got := texts(w.messages(c)); !slices.Equal(got, []string{"to someone else"}) {
		t.Errorf("C's messages %v", got)
	}
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"original"}) {
		t.Errorf("B's messages %v", got)
	}
	if inboxOutcomes(t, "dead")-deadBefore != 1 {
		t.Errorf("dead outcomes %v", inboxOutcomes(t, "dead")-deadBefore)
	}
	rows, err := mqoutbox.List(ctx, itCheck, mqoutbox.ListFilter{Id: dead})
	if err != nil || len(rows) != 1 || rows[0].Attempts != 3 || rows[0].Error != "mysql 1062" {
		t.Errorf("dead row %+v, %v", rows, err)
	}

	// The fix: the first copy unlinked, as the migration does.
	if _, err = itCheck.Exec("UPDATE messages SET dialog_message_id = -id WHERE user_id = ? AND dialog_message_id = ?", b, dup); err != nil {
		t.Fatal(err)
	}
	// A new leader replays every dead lane.
	itDelivery.relay.Stop()
	itDelivery.relay.Start()
	waitState(t, parked, userbox.EventPending, userbox.EventPublished)
	w.pub.drain()
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"original", "copy written twice", "later"}) {
		t.Errorf("B's messages after the replay %v", got)
	}

	// The operator: a dead event discarded, the lane replayed.
	again, err := mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, b, a, dup, "the same copy again"))
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, again, userbox.EventDead)
	last, err := mqoutbox.Insert(ctx, w.d.BoxDB, sendEvent(t, b, a, newDialogMessageId(), "last"))
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, last, userbox.EventParked)
	if info, err := mqoutbox.Discard(ctx, itCheck, again); err != nil || info.Recipient != b {
		t.Fatalf("discard: %+v, %v", info, err)
	}
	if n, err := mqoutbox.Replay(ctx, itCheck, b); err != nil || n != 1 {
		t.Fatalf("replay: %d, %v", n, err)
	}
	w.pub.drain()
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"original", "copy written twice", "later", "last"}) {
		t.Errorf("B's messages after the operator %v", got)
	}
	if n := countRowsOf(t, b); n != 0 {
		t.Errorf("%d rows of B left", n)
	}
}

// The consumer stops after the recipient's transaction committed and before
// the offset was marked: the copy comes again and finds no row.
func TestConsumerStopsBeforeMark(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	var crashed atomic.Bool
	target := atomic.Int64{}
	itDelivery.restartInbox(func(m *mqconsumer.Message) {
		if m.Header(mqoutbox.HeaderEventId) == strconv.FormatInt(target.Load(), 10) && crashed.CompareAndSwap(false, true) {
			runtime.Goexit() // the claim's goroutine dies here, as with kill -9
		}
	})
	defer itDelivery.restartInbox(nil)
	before := inboxOutcomes(t, "duplicate")
	ev := sendEvent(t, b, a, newDialogMessageId(), "once")
	id, err := mqoutbox.Insert(ctx, w.d.BoxDB, ev)
	if err != nil {
		t.Fatal(err)
	}
	target.Store(id)
	mqoutbox.Notify()
	w.pub.drain()
	eventually(t, 30*time.Second, "the copy handled again", func() bool { return crashed.Load() && inboxOutcomes(t, "duplicate")-before >= 1 })
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"once"}) {
		t.Errorf("B's messages %v", got)
	}
	w.checkBox(b)
}

// A second instance joins the group in the middle of a stream and leaves:
// every message lands once, in each sender's order. A restarted instance
// with the same static membership gets its partitions back at once.
func TestRebalance(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	senders := []int64{newUser(), newUser(), newUser()}
	recipients := []int64{newUser(), newUser(), newUser(), newUser()}
	const perPair = 15
	var wg sync.WaitGroup
	for _, s := range senders {
		wg.Go(func() {
			for i := range perPair {
				for _, r := range recipients {
					w.send(s, mtproto.PEER_USER, r, fmt.Sprintf("%d %02d", s, i))
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
	h, p := inbox_helper.Handler(w.d)
	second := mqconsumer.New(mqconsumer.Conf{Brokers: itKafka.Brokers, Topic: "Inbox-T", Group: inboxGroup, InstanceId: inboxInstance + "-2"}, "msg", h, p)
	time.Sleep(300 * time.Millisecond)
	second.Start()
	time.Sleep(2 * time.Second)
	// A static member does not leave: its partitions wait for it for the
	// session timeout, then go to the first instance.
	second.Stop()
	wg.Wait()
	w.pub.drain()
	eventually(t, 30*time.Second, "all partitions back", func() bool { return assigned(t) == mqoutbox.Partitions })
	for _, r := range recipients {
		got := w.messages(r)
		if len(got) != len(senders)*perPair {
			t.Errorf("user %d has %d messages, want %d", r, len(got), len(senders)*perPair)
		}
		last := map[int64]string{}
		for _, m := range got {
			if m.Text <= last[m.Sender] {
				t.Errorf("user %d: %q after %q", r, m.Text, last[m.Sender])
			}
			last[m.Sender] = m.Text
		}
		w.checkBox(r)
	}

	// The same instance again: its partitions come back without waiting for
	// the coordinator to drop the old member.
	itDelivery.restartInbox(nil)
	a, b := newUser(), newUser()
	start := time.Now()
	w.send(a, mtproto.PEER_USER, b, "after the restart")
	eventually(t, 5*time.Second, "applied after the restart", func() bool { return len(w.messages(b)) == 1 })
	t.Logf("applied %v after the instance restarted", time.Since(start).Round(10*time.Millisecond))
}

// A copy whose id belongs to another event (a reused id) is not applied.
func TestForeignCopy(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	before := inboxOutcomes(t, "mismatch")
	id, createdAt := insertEvent(t, sendEvent(t, b, a, newDialogMessageId(), "mine"), userbox.EventPublished)
	copyEvent(t, id, b, createdAt-60_000)
	eventually(t, 10*time.Second, "mismatch", func() bool { return inboxOutcomes(t, "mismatch")-before >= 1 })
	if n := len(w.messages(b)); n != 0 {
		t.Fatalf("a foreign copy applied: %d messages", n)
	}
	copyEvent(t, id, b, createdAt)
	w.pub.drain()
	if got := texts(w.messages(b)); !slices.Equal(got, []string{"mine"}) {
		t.Errorf("B's messages %v", got)
	}
}

// A basic group of 200: one send, an event per member, each applied once.
func TestBasicGroupOf200(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	members := make([]int64, 200)
	for i := range members {
		members[i] = newUser()
	}
	chat := newChat()
	w.chats.members[chat] = members
	w.send(members[0], mtproto.PEER_CHAT, chat, "hello all")
	w.pub.drain()
	for _, u := range members[1:] {
		if got := texts(w.messages(u)); !slices.Equal(got, []string{"hello all"}) {
			t.Fatalf("user %d messages %v", u, got)
		}
	}
}

// MySQL frozen in the middle of a stream of sends: the senders repeat
// what failed with the same random_id, as clients do. Every message lands
// once, nothing goes dead or parks: a pause of the database is temporary.
func TestMySQLPaused(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	senders := []int64{newUser(), newUser(), newUser()}
	b := newUser()
	const perSender = 30
	deadBefore := inboxOutcomes(t, "dead") + inboxOutcomes(t, "parked")
	var wg sync.WaitGroup
	for _, s := range senders {
		wg.Go(func() {
			for i := range perSender {
				m := msg.MakeTLOutboxMessage(&msg.OutboxMessage{RandomId: randomId(), Message: textMessage(s, mtproto.MakePeerUser(b), fmt.Sprintf("%02d", i))}).To_OutboxMessage()
				for attempt := 0; ; attempt++ {
					rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_, err := w.rpc.MsgSendMessageV2(rctx, &msg.TLMsgSendMessageV2{UserId: s, AuthKeyId: s * 10, PeerType: mtproto.PEER_USER, PeerId: b,
						Message: []*msg.OutboxMessage{proto.Clone(m).(*msg.OutboxMessage)}})
					cancel()
					if err == nil {
						break
					}
					if attempt > 100 {
						t.Errorf("send %d of %d: %v", i, s, err)
						return
					}
					time.Sleep(200 * time.Millisecond)
				}
				time.Sleep(30 * time.Millisecond)
			}
		})
	}
	time.Sleep(500 * time.Millisecond)
	if err := testinfra.Pause(ctx, itDB.Container); err != nil {
		t.Fatal(err)
	}
	time.Sleep(8 * time.Second)
	if err := testinfra.Unpause(ctx, itDB.Container); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	w.pub.drain()
	got := w.messages(b)
	if len(got) != len(senders)*perSender {
		t.Errorf("B has %d messages, want %d", len(got), len(senders)*perSender)
	}
	for _, s := range senders {
		if n := len(w.messages(s)); n != perSender {
			t.Errorf("sender %d has %d messages, want %d", s, n, perSender)
		}
	}
	if n := inboxOutcomes(t, "dead") + inboxOutcomes(t, "parked") - deadBefore; n != 0 {
		t.Errorf("%v events dead or parked", n)
	}
	w.checkBox(b)
}

// replyOf is what a client takes from a send reply: per message, its id,
// random_id, pts and pts_count.
func replyOf(t testing.TB, r *mtproto.Updates) [][4]int64 {
	t.Helper()
	randoms := map[int32]int64{}
	var out [][4]int64
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
			randoms[u.GetId_INT32()] = u.GetRandomId()
		}
	}
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			id := u.GetMessage_MESSAGE().GetId()
			out = append(out, [4]int64{int64(id), randoms[id], int64(u.GetPts_INT32()), int64(u.GetPtsCount())})
		}
	}
	return out
}

// A send repeated with the same random_id (ADR 0012, p. 6): from two
// devices at once, or after the reply was lost and Redis emptied, the
// repeat gets the first reply and the message is there once. The same
// random_id in another dialog, or with other messages, is
// RANDOM_ID_DUPLICATE.
func TestRepeatedSend(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	rid := randomId()
	request := func(to int64, randomIds ...int64) *msg.TLMsgSendMessageV2 {
		var list []*msg.OutboxMessage
		for _, r := range randomIds {
			list = append(list, msg.MakeTLOutboxMessage(&msg.OutboxMessage{RandomId: r, Message: textMessage(a, mtproto.MakePeerUser(to), "once")}).To_OutboxMessage())
		}
		return &msg.TLMsgSendMessageV2{UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: to, Message: list}
	}

	var (
		wg      sync.WaitGroup
		replies [2]*mtproto.Updates
		errs    [2]error
	)
	for i := range 2 {
		wg.Go(func() { replies[i], errs[i] = w.rpc.MsgSendMessageV2(ctx, request(b, rid)) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	first := replyOf(t, replies[0])
	if len(first) != 1 || first[0][1] != rid || !slices.Equal(replyOf(t, replies[1]), first) {
		t.Fatalf("replies %v and %v", first, replyOf(t, replies[1]))
	}
	w.pub.drain()
	if n, m := len(w.messages(a)), len(w.messages(b)); n != 1 || m != 1 {
		t.Fatalf("A has %d messages, B %d", n, m)
	}

	// Much later, with Redis emptied: the same reply, nothing new.
	code, _, err := itRedis.Container.Exec(ctx, []string{"redis-cli", "--no-auth-warning", "-a", itRedis.Password, "FLUSHALL"}, tcexec.Multiplexed())
	if err != nil || code != 0 {
		t.Fatalf("FLUSHALL: exit %d, %v", code, err)
	}
	pts := w.state(a).Pts
	r, err := w.rpc.MsgSendMessageV2(ctx, request(b, rid))
	if err != nil {
		t.Fatal(err)
	}
	if got := replyOf(t, r); !slices.Equal(got, first) {
		t.Errorf("repeat %v, first %v", got, first)
	}
	if w.state(a).Pts != pts {
		t.Error("the repeat took a pts")
	}

	for _, c := range []struct {
		name string
		req  *msg.TLMsgSendMessageV2
	}{
		{"another dialog", request(c, rid)},
		{"with another message", request(b, rid, randomId())},
		{"twice in one request", request(b, rid+1, rid+1)},
	} {
		if _, err = w.rpc.MsgSendMessageV2(ctx, c.req); !errors.Is(err, mtproto.ErrRandomIdDuplicate) {
			t.Errorf("%s: %v, want RANDOM_ID_DUPLICATE", c.name, err)
		}
	}
	w.pub.drain()
	if n, m := len(w.messages(a)), len(w.messages(b)); n != 1 || m != 1 {
		t.Errorf("A has %d messages, B %d", n, m)
	}
	w.checkBox(a)
}
