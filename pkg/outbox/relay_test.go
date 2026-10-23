package outbox

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

func header(m *sarama.ProducerMessage, name string) string {
	for _, h := range m.Headers {
		if string(h.Key) == name {
			return string(h.Value)
		}
	}
	return ""
}

func eventIds(msgs []*sarama.ProducerMessage) []int64 {
	var out []int64
	for _, m := range msgs {
		id, _ := strconv.ParseInt(header(m, HeaderEventId), 10, 64)
		out = append(out, id)
	}
	return out
}

func TestRelayPublishesInIdOrder(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	var ids []int64
	for i := range 20 {
		topic := "Inbox-T"
		if i%3 == 0 {
			topic = "Sync-T"
		}
		ids = append(ids, st.add(topic, int64(100+i%4), userbox.EventPending))
	}
	r.step(ctx)
	r.waitFinished()

	taken := k.taken()
	if got := eventIds(taken); !slices.Equal(got, ids) {
		t.Fatalf("published %v, want %v", got, ids)
	}
	for _, m := range taken {
		id, _ := strconv.ParseInt(header(m, HeaderEventId), 10, 64)
		row, ok := st.get(id)
		key, _ := m.Key.Encode()
		value, _ := m.Value.Encode()
		switch m.Topic {
		case "Inbox-T":
			// The broker has it; the recipient's transaction deletes it.
			if !ok || row.state != userbox.EventPublished || row.offset != m.Offset || row.partition != m.Partition {
				t.Errorf("inbox row %d after publication: %+v", id, row)
			}
			if len(value) != 0 {
				t.Errorf("inbox copy %d carries a body of %d bytes", id, len(value))
			}
			if due := time.UnixMilli(row.dueAt); due.Before(time.Now().Add(firstCheckDelay - time.Minute)) {
				t.Errorf("inbox row %d checked at %v", id, due)
			}
		case "Sync-T":
			if ok {
				t.Errorf("push row %d kept after publication", id)
			}
			if string(value) != "{}" {
				t.Errorf("push %d body %q", id, value)
			}
		}
		if want := strconv.FormatInt(row.recipient, 10); ok && string(key) != want {
			t.Errorf("key %q, want %s", key, want)
		}
		if header(m, HeaderMethod) == "" || header(m, HeaderCreatedAt) == "" || header(m, HeaderInitiator) != "1" {
			t.Errorf("headers %v", m.Headers)
		}
	}
}

// A row the broker did not take stays pending and goes first in the next
// round; the rows it took are not published again.
func TestRelayPartialFailure(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	var ids []int64
	for range 6 {
		ids = append(ids, st.add("Inbox-T", 7, userbox.EventPending))
	}
	failing := ids[2]
	k.setDecide(func(m *sarama.ProducerMessage) verdict {
		if header(m, HeaderEventId) == strconv.FormatInt(failing, 10) {
			return verdict{err: sarama.ErrNotEnoughReplicas}
		}
		return verdict{}
	})
	if wait := r.step(ctx); wait != 0 {
		t.Errorf("a round with progress waits %v", wait)
	}
	r.waitFinished()
	if row, _ := st.get(failing); row.state != userbox.EventPending {
		t.Fatalf("failed row %+v", row)
	}
	k.setDecide(nil)
	r.step(ctx)
	r.waitFinished()
	got := eventIds(k.taken())
	want := append(slices.Delete(slices.Clone(ids), 2, 3), failing)
	if !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}

// A row the broker refuses for good leaves pending in one round: an inbox
// row goes dead, a push row is dropped; the other rows go on, without a
// pause.
func TestRelayPermanentErrors(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	a := st.add("Inbox-T", 7, userbox.EventPending)
	bigInbox := st.add("Inbox-T", 7, userbox.EventPending)
	bigPush := st.add("Sync-T", 7, userbox.EventPending)
	b := st.add("Inbox-T", 8, userbox.EventPending)
	k.setDecide(func(m *sarama.ProducerMessage) verdict {
		switch header(m, HeaderEventId) {
		case strconv.FormatInt(bigInbox, 10):
			return verdict{err: sarama.ErrMessageSizeTooLarge}
		case strconv.FormatInt(bigPush, 10):
			return verdict{err: sarama.ConfigurationError("Attempt to produce message larger than configured Producer.MaxMessageBytes")}
		}
		return verdict{}
	})
	if wait := r.step(ctx); wait != 0 {
		t.Errorf("a round with progress waits %v", wait)
	}
	r.waitFinished()
	if row, _ := st.get(bigInbox); row.state != userbox.EventDead {
		t.Errorf("refused inbox row %+v", row)
	}
	if _, ok := st.get(bigPush); ok {
		t.Error("refused push row kept")
	}
	if got := eventIds(k.taken()); !slices.Equal(got, []int64{a, b}) {
		t.Errorf("published %v", got)
	}
	// Nothing is left pending for the next round.
	before := len(k.taken())
	r.step(ctx)
	if len(k.taken()) != before {
		t.Error("published again")
	}
}

// Rows of a topic without its partitions wait; the others go.
func TestRelayTopicNotReady(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	k.partitions["Sync-T"] = 3
	r := testRelay(st, k)
	ctx := context.Background()
	push := st.add("Sync-T", 7, userbox.EventPending)
	inbox := st.add("Inbox-T", 7, userbox.EventPending)
	r.step(ctx)
	r.waitFinished()
	if got := eventIds(k.taken()); !slices.Equal(got, []int64{inbox}) {
		t.Fatalf("published %v", got)
	}
	if row, ok := st.get(push); !ok || row.state != userbox.EventPending {
		t.Fatalf("push of a topic not ready: %+v", row)
	}
	k.mu.Lock()
	k.partitions["Sync-T"] = Partitions
	k.mu.Unlock()
	r.checkTopics()
	r.step(ctx)
	r.waitFinished()
	if got := eventIds(k.taken()); !slices.Equal(got, []int64{inbox, push}) {
		t.Fatalf("published %v", got)
	}
}

// A topic error marks the topic not ready at once and asks for a check.
func TestRelayTopicError(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	st.add("Sync-T", 7, userbox.EventPending)
	k.setDecide(func(m *sarama.ProducerMessage) verdict { return verdict{err: sarama.ErrUnknownTopicOrPartition} })
	r.step(ctx)
	if r.ready["Sync-T"] || !r.checkNow {
		t.Errorf("ready %v, check %v", r.ready, r.checkNow)
	}
}

// A broker that does not answer ends the round at the limit; the producer is
// made again and the rows stay pending.
func TestRelayRoundTimeout(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	id := st.add("Inbox-T", 7, userbox.EventPending)
	k.setDecide(func(*sarama.ProducerMessage) verdict { return verdict{silent: true} })
	start := time.Now()
	wait := r.step(ctx)
	if d := time.Since(start); d > r.roundTimeout+time.Second {
		t.Errorf("round took %v", d)
	}
	if wait < pauseMin {
		t.Errorf("a round without progress waits %v", wait)
	}
	if _, dropped := k.counts(); dropped != 1 {
		t.Errorf("%d producers dropped", dropped)
	}
	if row, _ := st.get(id); row.state != userbox.EventPending {
		t.Errorf("row %+v", row)
	}
	k.setDecide(nil)
	r.step(ctx)
	r.waitFinished()
	if made, _ := k.counts(); made != 2 || !slices.Equal(eventIds(k.taken()), []int64{id}) {
		t.Errorf("%d producers, published %v", made, eventIds(k.taken()))
	}
}

// Rows answered but not yet recorded are not selected again.
func TestRelayInflight(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	st.finishGate = make(chan struct{})
	r := testRelay(st, k)
	ctx := context.Background()
	a := st.add("Inbox-T", 7, userbox.EventPending)
	r.step(ctx)
	b := st.add("Inbox-T", 7, userbox.EventPending)
	r.step(ctx)
	close(st.finishGate)
	r.waitFinished()
	if got := eventIds(k.taken()); !slices.Equal(got, []int64{a, b}) {
		t.Fatalf("published %v", got)
	}
}

// What the consumer did to a row while it was being published (deferred
// it: a new revision) is not overwritten by the relay's record.
func TestRelayRevision(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	st.finishGate = make(chan struct{})
	r := testRelay(st, k)
	ctx := context.Background()
	id := st.add("Inbox-T", 7, userbox.EventPending)
	r.step(ctx)
	st.mu.Lock()
	st.rows[id].revision++
	st.rows[id].state = userbox.EventPending
	st.mu.Unlock()
	close(st.finishGate)
	r.waitFinished()
	if row, _ := st.get(id); row.state != userbox.EventPending || row.published != 0 {
		t.Errorf("row %+v: the stale answer was recorded", row)
	}
}

// A relay that lost its lock publishes nothing.
func TestRelayLosesLock(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	r.step(ctx)
	if !r.leader {
		t.Fatal("not the leader")
	}
	st.mu.Lock()
	st.lostLock = true
	st.mu.Unlock()
	st.add("Inbox-T", 7, userbox.EventPending)
	if wait := r.step(ctx); wait != leaderRetry {
		t.Errorf("wait %v", wait)
	}
	if r.leader || len(k.taken()) != 0 {
		t.Errorf("leader %v, published %d", r.leader, len(k.taken()))
	}
}

// Taking the lock returns the published rows to pending and replays every
// dead lane, due or not.
func TestRelayLead(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	published := st.add("Inbox-T", 7, userbox.EventPublished)
	dead := st.add("Inbox-T", 8, userbox.EventDead)
	parked := st.add("Inbox-T", 8, userbox.EventParked)
	st.mu.Lock()
	st.rows[dead].dueAt = time.Now().Add(time.Hour).UnixMilli()
	st.mu.Unlock()
	if !r.lead(context.Background()) {
		t.Fatal("not the leader")
	}
	for _, id := range []int64{published, dead, parked} {
		if row, _ := st.get(id); row.state != userbox.EventPending {
			t.Errorf("row %d: %v", id, row.state)
		}
	}
	if row, _ := st.get(dead); row.replays != 1 {
		t.Errorf("replays %d", row.replays)
	}
	// A second relay waits.
	if newRelay(st, k.dial, RelayConf{}, "standby").lead(context.Background()) {
		t.Error("two leaders")
	}
}

// The redelivery timer: a copy passed or lost is published again, a copy
// not reached yet is looked at later.
func TestRelayRedelivery(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	r.step(ctx) // lead, connect
	now := time.Now()
	r.now = func() time.Time { return now }
	mk := func(partition int32, offset int64) int64 {
		id := st.add("Inbox-T", 7, userbox.EventPublished)
		st.mu.Lock()
		st.rows[id].partition, st.rows[id].offset, st.rows[id].dueAt = partition, offset, now.Add(-time.Second).UnixMilli()
		st.mu.Unlock()
		return id
	}
	k.mu.Lock()
	for p := range int32(4) {
		k.logs[logKey("Inbox-T", p)] = make([]*sarama.ProducerMessage, 10)
	}
	k.first[logKey("Inbox-T", 2)] = 5
	k.committed["Inbox-T"] = map[int32]int64{0: 4, 1: 3}
	k.mu.Unlock()
	skipped := mk(0, 3) // committed 4 > 3
	ahead := mk(1, 5)   // committed 3 <= 5, in the log
	lost := mk(2, 2)    // log starts at 5
	beyond := mk(3, 10) // log ends at 10
	notDue := mk(1, 1)
	st.mu.Lock()
	st.rows[notDue].dueAt = now.Add(time.Minute).UnixMilli()
	st.mu.Unlock()

	r.redeliver(ctx)
	for id, want := range map[int64]userbox.EventState{skipped: userbox.EventPending, lost: userbox.EventPending, beyond: userbox.EventPending, ahead: userbox.EventPublished, notDue: userbox.EventPublished} {
		row, _ := st.get(id)
		if row.state != want {
			t.Errorf("row %d: %v, want %v", id, row.state, want)
		}
		if want == userbox.EventPending && row.redeliveries != 1 {
			t.Errorf("row %d: %d redeliveries", id, row.redeliveries)
		}
	}
	if row, _ := st.get(ahead); time.UnixMilli(row.dueAt) != now.Add(recheckDelay).Truncate(time.Millisecond) {
		t.Errorf("copy not reached: next check %v", time.UnixMilli(row.dueAt))
	}
}

func TestFateOf(t *testing.T) {
	for _, c := range []struct {
		offset, committed, first, end int64
		want                          copyFate
	}{
		{5, 6, 0, 10, copySkipped},
		{5, 5, 0, 10, copyAhead}, // the copy is the next to consume
		{5, -1, 0, 10, copyAhead},
		{5, -1, 6, 10, copyLost},
		{5, -1, 0, 5, copyLost},
		{5, 3, 0, 4, copyLost},
	} {
		if got := fateOf(c.offset, c.committed, c.first, c.end); got != c.want {
			t.Errorf("fateOf(%d, %d, %d, %d) = %v, want %v", c.offset, c.committed, c.first, c.end, got, c.want)
		}
	}
}

func TestCheckDelay(t *testing.T) {
	for n, want := range []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		if got := checkDelay(n); got != want {
			t.Errorf("checkDelay(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestProducerConfig(t *testing.T) {
	c := ProducerConfig("test")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if !c.Producer.Idempotent || c.Net.MaxOpenRequests != 1 || c.Producer.RequiredAcks != sarama.WaitForAll || c.Metadata.AllowAutoTopicCreation {
		t.Errorf("config %+v", c.Producer)
	}
	// The broker answers before the client gives up reading, and the round
	// outlasts the producer's own retries.
	if c.Net.ReadTimeout <= c.Producer.Timeout {
		t.Errorf("read timeout %v <= producer timeout %v", c.Net.ReadTimeout, c.Producer.Timeout)
	}
	if RoundTimeout <= c.Producer.Timeout {
		t.Errorf("round %v <= producer timeout %v", RoundTimeout, c.Producer.Timeout)
	}
	if MaxMessageBytes < userbox.MaxEventBytes+1<<20 {
		t.Errorf("max message %d leaves no room over an event of %d", MaxMessageBytes, userbox.MaxEventBytes)
	}
}

func TestClassifySend(t *testing.T) {
	for _, c := range []struct {
		err  error
		want sendClass
	}{
		{sarama.ErrMessageSizeTooLarge, sendPermanent},
		{sarama.ErrInvalidMessage, sendPermanent},
		{sarama.ErrInvalidRecord, sendPermanent},
		{sarama.ConfigurationError("too large"), sendPermanent},
		{sarama.PacketEncodingError{Info: "bad"}, sendPermanent},
		{sarama.ErrUnknownTopicOrPartition, sendTopic},
		{sarama.ErrTopicAuthorizationFailed, sendTopic},
		{sarama.ErrNotEnoughReplicas, sendTransient},
		{sarama.ErrOutOfBrokers, sendTransient},
		{errFake, sendTransient},
	} {
		if got := classifySend(c.err); got != c.want {
			t.Errorf("classifySend(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

// Stop ends the current round within StopRoundWait and releases the lock.
func TestRelayStop(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := newRelay(st, k.dial, RelayConf{}, "test")
	k.setDecide(func(*sarama.ProducerMessage) verdict { return verdict{silent: true} })
	st.add("Inbox-T", 7, userbox.EventPending)
	r.Start()
	time.Sleep(200 * time.Millisecond) // in the round, waiting for answers
	start := time.Now()
	r.Stop()
	if d := time.Since(start); d > StopRoundWait+time.Second {
		t.Errorf("Stop took %v", d)
	}
	if ok, _ := st.Leading(context.Background()); ok || !st.lockFree {
		t.Error("the lock is still held")
	}
}

// The automatic replay takes the dead lanes due by their due_at, with their
// parked rows, and leaves the others for later.
func TestRelayAutoReplay(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	now := time.Now()
	r.now = func() time.Time { return now }
	due := st.add("Inbox-T", 7, userbox.EventDead)
	parked := st.add("Inbox-T", 7, userbox.EventParked)
	later := st.add("Inbox-T", 8, userbox.EventDead)
	st.mu.Lock()
	st.rows[due].dueAt = now.Add(-time.Second).UnixMilli()
	st.rows[later].dueAt = now.Add(time.Minute).UnixMilli()
	st.mu.Unlock()
	r.replay(context.Background(), false)
	for id, want := range map[int64]userbox.EventState{due: userbox.EventPending, parked: userbox.EventPending, later: userbox.EventDead} {
		if row, _ := st.get(id); row.state != want {
			t.Errorf("row %d: %v, want %v", id, row.state, want)
		}
	}
	if row, _ := st.get(due); row.replays != 1 {
		t.Errorf("replays %d", row.replays)
	}
}

// TestTimersAfterTheBrokerIsGone: the topic check drops the connection when
// the broker is gone; the redelivery and lag checks due in the same pass
// must not use it (they did: a nil dereference crashed msg).
func TestTimersAfterTheBrokerIsGone(t *testing.T) {
	k := newFakeKafka()
	r := testRelay(newFakeStore(), k)
	if wait := r.connect(); wait != 0 || r.kafka == nil {
		t.Fatalf("connect: wait %v, connected %v", wait, r.kafka != nil)
	}
	k.mu.Lock()
	k.metaErr = errFake
	k.mu.Unlock()
	r.due = map[string]time.Time{} // every timer due
	r.timers(context.Background(), true)
	if r.kafka != nil {
		t.Fatal("connection kept after the topic check failed")
	}
}

// TestRoundAfterTheTopicCheckDroppedTheConnection: a topic check due in the
// same step drops the connection when the broker is gone; the round must
// not publish on it (it did: a nil dereference crashed msg).
func TestRoundAfterTheTopicCheckDroppedTheConnection(t *testing.T) {
	st, k := newFakeStore(), newFakeKafka()
	r := testRelay(st, k)
	ctx := context.Background()
	st.add("Inbox-T", 7, userbox.EventPending)
	r.step(ctx) // lead, connect, publish
	r.waitFinished()

	st.add("Inbox-T", 7, userbox.EventPending)
	k.mu.Lock()
	k.metaErr = errFake
	k.mu.Unlock()
	r.checkNow = true
	if wait := r.step(ctx); wait <= 0 {
		t.Fatalf("step after a dropped connection: wait %v, want the reconnect pause", wait)
	}
	if r.kafka != nil {
		t.Fatal("connection kept after the topic check failed")
	}
	if n := len(k.taken()); n != 1 {
		t.Fatalf("%d messages published, want the first one only", n)
	}
}
