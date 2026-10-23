package outbox

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// RelayConf is the relay's part of a service config.
type RelayConf struct {
	Brokers []string
	// Groups are the consumer groups of the topics: the relay reports their
	// lag, and checks against the group of an inbox topic that a published
	// copy was consumed (redelivery).
	Groups []GroupConf `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// GroupConf names the consumer group of a topic.
type GroupConf struct {
	Topic string
	Group string
}

// Time limits and periods of the relay (ADR 0012, p. 3).
const (
	// RoundTimeout bounds the wait for the broker's answers in a round:
	// then the producer is dropped and made again, and what is not
	// answered stays pending.
	RoundTimeout = 10 * time.Second
	// StopRoundWait is how long Stop lets the current round finish.
	StopRoundWait = 2 * time.Second

	pollInterval       = 50 * time.Millisecond // rows of other processes
	leaderRetry        = 5 * time.Second       // a standby tries the lock
	connectMin         = time.Second
	connectMax         = 30 * time.Second
	pauseMin           = 100 * time.Millisecond // a round without progress
	pauseMax           = 5 * time.Second
	topicCheckInterval = 30 * time.Second
	redeliveryInterval = 10 * time.Second
	replayInterval     = time.Minute
	countsInterval     = 10 * time.Second
	queryTimeout       = 5 * time.Second
	resetTimeout       = time.Minute      // published rows back to pending on taking the lock
	checkTimeout       = 30 * time.Second // a pass of the redelivery timer

	roundRows    = 500
	roundPayload = 16 << 20

	// The first check of a published copy; each redelivery doubles it, up
	// to maxCheckDelay. A copy the consumer has not reached is looked at
	// again after recheckDelay.
	firstCheckDelay = 2 * time.Minute
	maxCheckDelay   = time.Hour
	recheckDelay    = time.Minute
	checkBatch      = 1000
)

var errNotLeader = errors.New("outbox: not the relay leader")

// Relay publishes the rows of mq_outbox (see the package documentation).
type Relay struct {
	conf     RelayConf
	store    store
	dial     dialer
	clientID string
	now      func() time.Time
	log      logx.Logger
	// roundTimeout is RoundTimeout; tests shorten it.
	roundTimeout time.Duration

	// The loop's own state.
	leader     bool
	kafka      kafkaConn
	ready      map[string]bool
	checkNow   bool // a topic error asks for a check before the next round
	connectAt  time.Time
	connectGap time.Duration
	pause      time.Duration
	due        map[string]time.Time // next run of each timer

	mu       sync.Mutex
	inflight map[int64]bool // answered rows the finisher has not recorded

	jobs   chan finishJob
	cancel context.CancelFunc
	done   chan struct{}
}

type finishJob struct {
	f   finish
	ids []int64
}

// NewRelay returns a relay of the database of db, which must be a pool
// box transactions may run on (userbox.Check): it replays lanes in box
// transactions.
func NewRelay(db *sql.DB, conf RelayConf) *Relay {
	host, _ := os.Hostname()
	return newRelay(newSQLStore(db), dialSarama, conf, "outbox-relay-"+host)
}

func newRelay(st store, dial dialer, conf RelayConf, clientID string) *Relay {
	return &Relay{
		conf:         conf,
		store:        st,
		dial:         dial,
		clientID:     clientID,
		now:          time.Now,
		log:          logx.WithContext(context.Background()),
		roundTimeout: RoundTimeout,
		ready:        map[string]bool{},
		due:          map[string]time.Time{},
		inflight:     map[int64]bool{},
	}
}

// Start runs the relay in the background until Stop.
func (r *Relay) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	r.jobs = make(chan finishJob, 16)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		r.finisher()
	}()
	go func() {
		defer close(r.done)
		r.run(ctx)
		close(r.jobs)
		<-finished
		r.resign()
	}()
}

// Stop starts no new round, lets the current one finish within
// StopRoundWait, records its answers, drops the producer and releases the
// lock. What is not answered stays pending.
func (r *Relay) Stop() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
}

func (r *Relay) run(ctx context.Context) {
	wake, unsubscribe := subscribe()
	defer unsubscribe()
	for ctx.Err() == nil {
		wait := r.step(ctx)
		if wait <= 0 {
			continue
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-wake:
		case <-t.C:
		}
		t.Stop()
	}
}

// step does one thing of the loop and says how long to wait before the
// next (a wake-up cuts the wait short).
func (r *Relay) step(ctx context.Context) time.Duration {
	if !r.leader {
		if !r.lead(ctx) {
			return leaderRetry
		}
	}
	dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
	leading, err := r.store.Leading(dbctx)
	cancel()
	if err != nil || !leading {
		r.log.Errorf("outbox: relay lost the lock (%v), publishing stops", err)
		r.resign()
		return leaderRetry
	}
	r.timers(ctx, false)
	if r.kafka == nil {
		if wait := r.connect(); wait > 0 {
			return wait
		}
	}
	r.timers(ctx, true)
	if r.checkNow {
		r.checkTopics()
	}
	// The checks above drop the connection on a broker error: publish only
	// after the next connection.
	if r.kafka == nil {
		return max(r.connectAt.Sub(r.now()), 0)
	}
	selected, progress := r.round(ctx)
	switch {
	case selected == 0:
		r.pause = 0
		return pollInterval
	case progress == 0:
		r.pause = min(max(2*r.pause, pauseMin), pauseMax)
		return r.pause
	}
	r.pause = 0
	return 0
}

// lead tries to take the lock. On success the relay first returns every
// published row to pending, which covers a lost host, Kafka volume or
// offsets and a restored database without waiting for the timer, and
// replays every dead lane.
func (r *Relay) lead(ctx context.Context) bool {
	dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
	defer cancel()
	ok, err := r.store.Lead(dbctx)
	if err != nil {
		r.log.Errorf("outbox: take the relay lock: %v", err)
	}
	if !ok {
		return false
	}
	// In batches of rows, however many a long stop of a consumer left.
	rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), resetTimeout)
	defer rcancel()
	n, err := r.store.ResetPublished(rctx)
	if err != nil {
		r.log.Errorf("outbox: return published rows to pending: %v", err)
		r.store.Resign()
		return false
	}
	if n > 0 {
		metricRedelivered.Add(float64(n), "all", "leader")
	}
	r.leader = true
	metricLeader.Set(1)
	r.log.Infof("outbox: relay leads, %d published rows returned to pending", n)
	r.replay(ctx, true)
	now := r.now()
	for _, name := range []string{"replay", "redelivery", "counts", "topics"} {
		r.due[name] = now
	}
	return true
}

func (r *Relay) resign() {
	if r.kafka != nil {
		r.kafka.Close()
		r.kafka = nil
	}
	metricConnected.Set(0)
	r.ready = map[string]bool{}
	for t := range userbox.Topics() {
		metricTopicReady.Set(0, t)
	}
	if r.leader {
		r.store.Resign()
		r.leader = false
		metricLeader.Set(0)
	}
}

// connect makes the Kafka connection, with a growing pause between
// failures; it returns the time to wait, 0 when connected.
func (r *Relay) connect() time.Duration {
	now := r.now()
	if now.Before(r.connectAt) {
		return r.connectAt.Sub(now)
	}
	k, err := r.dial(r.conf.Brokers, r.clientID)
	if err != nil {
		r.connectGap = min(max(2*r.connectGap, connectMin), connectMax)
		r.connectAt = now.Add(r.connectGap)
		r.log.Errorf("outbox: connect to Kafka (next try in %v): %v", r.connectGap, err)
		return r.connectGap
	}
	r.kafka = k
	r.connectGap = 0
	metricConnected.Set(1)
	r.checkTopics()
	return 0
}

func (r *Relay) disconnect(err error) {
	r.log.Errorf("outbox: Kafka connection dropped: %v", err)
	if r.kafka != nil {
		r.kafka.Close()
		r.kafka = nil
	}
	metricConnected.Set(0)
	r.connectGap = connectMin
	r.connectAt = r.now().Add(r.connectGap)
}

// checkTopics compares the registered topics with the broker: a topic
// without the expected partitions is not ready, and its rows wait.
func (r *Relay) checkTopics() {
	r.checkNow = false
	r.due["topics"] = r.now().Add(topicCheckInterval)
	if r.kafka == nil {
		return
	}
	topics := sortedTopics()
	counts, errs, err := r.kafka.Partitions(topics)
	if err != nil {
		r.disconnect(err)
		return
	}
	for _, t := range topics {
		ok := errs[t] == nil && counts[t] == Partitions
		if was, seen := r.ready[t]; !ok && (was || !seen) {
			r.log.Errorf("outbox: topic %s is not ready: %d partitions, want %d (%v); its rows wait", t, counts[t], Partitions, errs[t])
		}
		r.ready[t] = ok
		gaugeBool(metricTopicReady, ok, t)
	}
}

func sortedTopics() []string {
	var out []string
	for t := range userbox.Topics() {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

func (r *Relay) readyTopics() []string {
	var out []string
	for t, ok := range r.ready {
		if ok {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// timers runs the periodic work that is due: without Kafka the counts and
// the replay of dead lanes, with it the topic check and the redelivery
// check.
func (r *Relay) timers(ctx context.Context, connected bool) {
	now := r.now()
	run := func(name string, every time.Duration, f func()) {
		if t, ok := r.due[name]; ok && now.Before(t) {
			return
		}
		r.due[name] = now.Add(every)
		f()
	}
	if !connected {
		run("counts", countsInterval, func() { r.counts(ctx) })
		run("replay", replayInterval, func() { r.replay(ctx, false) })
		return
	}
	// A step may drop the connection (checkTopics on a broker error): the
	// ones after it in this pass wait for the next connection.
	connectedOnly := func(f func()) func() {
		return func() {
			if r.kafka != nil {
				f()
			}
		}
	}
	run("topics", topicCheckInterval, connectedOnly(r.checkTopics))
	run("redelivery", redeliveryInterval, connectedOnly(func() { r.redeliver(ctx) }))
	run("lag", countsInterval, connectedOnly(r.lag))
}

func nowMs() int64 { return time.Now().UnixMilli() }

// round publishes up to roundRows pending rows of the ready topics in id
// order and hands the answers to the finisher. It returns the rows it
// selected and those that left pending for good (taken by the broker or
// refused for good).
func (r *Relay) round(ctx context.Context) (selected, progress int) {
	topics := r.readyTopics()
	if len(topics) == 0 {
		return 0, 0
	}
	start := time.Now()
	dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
	defer cancel()
	rows, err := r.store.Pending(dbctx, topics, r.inflightSet(), roundRows)
	if err != nil {
		r.log.Errorf("outbox: select pending rows: %v", err)
		return 0, 0
	}
	if len(rows) == 0 {
		return 0, 0
	}
	// Push rows carry their payloads, up to roundPayload a round; the rows
	// from the first one over it go in the next round, so the order holds.
	var push []int64
	size := 0
	for i, row := range rows {
		if class, _ := userbox.TopicClassOf(row.topic); class != userbox.TopicPush {
			continue
		}
		if i > 0 && size+row.size > roundPayload {
			rows = rows[:i]
			break
		}
		size += row.size
		push = append(push, row.id)
	}
	payloads, err := r.store.Payloads(dbctx, push)
	if err != nil {
		r.log.Errorf("outbox: read payloads: %v", err)
		return 0, 0
	}
	// A push row deleted in between (pluma-admin outbox discard) is gone.
	rows = slices.DeleteFunc(rows, func(row row) bool {
		class, _ := userbox.TopicClassOf(row.topic)
		_, ok := payloads[row.id]
		return class == userbox.TopicPush && !ok
	})
	metricRoundSeconds.ObserveFloat(time.Since(start).Seconds(), "select")

	start = time.Now()
	answers := r.send(ctx, rows, payloads)
	metricRoundSeconds.ObserveFloat(time.Since(start).Seconds(), "send")
	return len(rows), r.settle(ctx, rows, answers)
}

// answer is the broker's answer about one row of a round.
type answer struct {
	done      bool // answered
	err       error
	partition int32
	offset    int64
	at        time.Time
}

// send produces the rows in order and collects the answers until all are
// in, RoundTimeout passes or, after Stop, StopRoundWait does. A producer
// that did not answer in time is dropped with what it still holds.
func (r *Relay) send(ctx context.Context, rows []row, payloads map[int64][]byte) []answer {
	answers := make([]answer, len(rows))
	if r.kafka == nil {
		return answers
	}
	p, err := r.kafka.Producer()
	if err != nil {
		r.disconnect(err)
		return answers
	}
	timeout := time.NewTimer(r.roundTimeout)
	defer timeout.Stop()
	stopping := ctx.Done()
	var stopped <-chan time.Time
	next, answered := 0, 0
	for answered < len(rows) {
		var (
			input chan<- *sarama.ProducerMessage
			msg   *sarama.ProducerMessage
		)
		if next < len(rows) {
			input, msg = p.Input(), message(rows[next], payloads, next)
		}
		select {
		case input <- msg:
			next++
		case m := <-p.Successes():
			if i, ok := m.Metadata.(int); ok && i < len(answers) && !answers[i].done {
				answers[i] = answer{done: true, partition: m.Partition, offset: m.Offset, at: time.Now()}
				answered++
			}
		case e := <-p.Errors():
			if i, ok := e.Msg.Metadata.(int); ok && i < len(answers) && !answers[i].done {
				answers[i] = answer{done: true, err: e.Err, at: time.Now()}
				answered++
			}
		case <-timeout.C:
			r.log.Errorf("outbox: %d of %d rows unanswered after %v, the producer is made again", len(rows)-answered, len(rows), r.roundTimeout)
			r.kafka.DropProducer()
			return answers
		case <-stopping:
			stopping = nil
			stopped = time.After(StopRoundWait)
		case <-stopped:
			r.kafka.DropProducer()
			return answers
		}
	}
	return answers
}

func message(r row, payloads map[int64][]byte, i int) *sarama.ProducerMessage {
	m := &sarama.ProducerMessage{
		Topic: r.topic,
		Key:   sarama.StringEncoder(strconv.FormatInt(r.recipient, 10)),
		Value: sarama.ByteEncoder(payloads[r.id]),
		Headers: []sarama.RecordHeader{
			{Key: []byte(HeaderMethod), Value: []byte(r.method)},
			{Key: []byte(HeaderEventId), Value: []byte(strconv.FormatInt(r.id, 10))},
			{Key: []byte(HeaderInitiator), Value: []byte(strconv.FormatInt(r.initiator, 10))},
			{Key: []byte(HeaderCreatedAt), Value: []byte(strconv.FormatInt(r.createdAt, 10))},
		},
		Timestamp: time.UnixMilli(r.createdAt),
		Metadata:  i,
	}
	if m.Value.Length() == 0 {
		m.Value = sarama.ByteEncoder{} // an inbox copy: the body is the row
	}
	if r.trace != "" {
		m.Headers = append(m.Headers, sarama.RecordHeader{Key: []byte(HeaderTraceParent), Value: []byte(r.trace)})
	}
	return m
}

// Headers of the records the relay writes.
const (
	HeaderMethod      = "method"
	HeaderEventId     = "event-id"
	HeaderInitiator   = "initiator"
	HeaderCreatedAt   = "created-at"
	HeaderTraceParent = "traceparent"
)

// settle sorts the answers of a round: what the broker took goes to the
// finisher, an inbox row refused for good goes dead, a push row refused
// for good is dropped; transient errors and rows not answered stay pending.
func (r *Relay) settle(ctx context.Context, rows []row, answers []answer) int {
	var (
		job      finishJob
		progress int
		now      = r.now()
	)
	for i, row := range rows {
		a := answers[i]
		if !a.done {
			continue
		}
		class, _ := userbox.TopicClassOf(row.topic)
		if a.err == nil {
			progress++
			metricPublished.Inc(row.topic)
			metricPublishDelay.ObserveFloat(a.at.Sub(time.UnixMilli(row.createdAt)).Seconds(), row.topic)
			job.ids = append(job.ids, row.id)
			if class == userbox.TopicInbox {
				job.f.published = append(job.f.published, ack{
					id: row.id, revision: row.revision, partition: a.partition, offset: a.offset,
					dueAt: now.Add(checkDelay(row.redeliveries)).UnixMilli(),
				})
			} else {
				job.f.deleted = append(job.f.deleted, ref{id: row.id, revision: row.revision})
			}
			continue
		}
		switch classifySend(a.err) {
		case sendPermanent:
			progress++
			metricPublishErrors.Inc(row.topic, "permanent")
			if class == userbox.TopicInbox {
				r.log.Errorf("outbox: inbox event %d %s to user %d refused by the broker for good, dead: %v", row.id, row.method, row.recipient, a.err)
				dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
				if err := r.store.Dead(dbctx, row, "broker: "+a.err.Error()); err != nil {
					r.log.Errorf("outbox: make event %d dead: %v", row.id, err)
				}
				cancel()
				continue
			}
			r.log.Errorf("outbox: push %d %s to %d of %s refused by the broker for good, dropped: %v", row.id, row.method, row.recipient, row.topic, a.err)
			metricDropped.Inc(row.topic, "refused")
			job.ids = append(job.ids, row.id)
			job.f.deleted = append(job.f.deleted, ref{id: row.id, revision: row.revision})
		case sendTopic:
			metricPublishErrors.Inc(row.topic, "topic")
			if r.ready[row.topic] {
				r.log.Errorf("outbox: topic %s refused event %d: %v; checking the topics", row.topic, row.id, a.err)
			}
			r.ready[row.topic] = false
			metricTopicReady.Set(0, row.topic)
			r.checkNow = true
		default:
			metricPublishErrors.Inc(row.topic, "transient")
		}
	}
	if len(job.ids) > 0 {
		r.mu.Lock()
		for _, id := range job.ids {
			r.inflight[id] = true
		}
		r.mu.Unlock()
		r.jobs <- job
	}
	return progress
}

// checkDelay is when the redelivery timer first looks at a copy published
// after redeliveries redeliveries.
func checkDelay(redeliveries int) time.Duration {
	d := firstCheckDelay
	for range redeliveries {
		if d >= maxCheckDelay {
			break
		}
		d *= 2
	}
	return min(d, maxCheckDelay)
}

func (r *Relay) inflightSet() map[int64]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.inflight) == 0 {
		return nil
	}
	out := make(map[int64]bool, len(r.inflight))
	for id := range r.inflight {
		out[id] = true
	}
	return out
}

// finisher records the answers of the rounds, in their order, while the
// relay goes on with the next rounds. Rows it fails to record stay pending
// and are published again: their consumer applies them once.
func (r *Relay) finisher() {
	for job := range r.jobs {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		if err := r.store.Finish(ctx, job.f); err != nil {
			r.log.Errorf("outbox: record %d answers of the broker: %v; they will be published again", len(job.ids), err)
		}
		cancel()
		metricRoundSeconds.ObserveFloat(time.Since(start).Seconds(), "finish")
		r.mu.Lock()
		for _, id := range job.ids {
			delete(r.inflight, id)
		}
		r.mu.Unlock()
	}
}
