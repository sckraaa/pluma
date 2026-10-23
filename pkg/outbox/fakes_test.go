package outbox

import (
	"context"
	"errors"
	"hash/fnv"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

func init() {
	if err := Register(Topics{Inbox: []string{"Inbox-T"}, Push: []string{"Sync-T"}}); err != nil {
		panic(err)
	}
}

// fakeRow is an mq_outbox row of fakeStore.
type fakeRow struct {
	row
	payload   []byte
	state     userbox.EventState
	partition int32
	offset    int64
	dueAt     int64
	replays   int
	published int
}

// fakeStore is the store in memory, with the semantics of the SQL one.
type fakeStore struct {
	mu       sync.Mutex
	rows     map[int64]*fakeRow
	nextId   int64
	lockFree bool // another relay may take the lock
	leading  bool
	lostLock bool

	finishGate chan struct{} // Finish waits for it when set
	finishErr  error
	finishes   int
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[int64]*fakeRow{}, lockFree: true}
}

// add commits a row and returns its id.
func (s *fakeStore) add(topic string, recipient int64, state userbox.EventState) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextId++
	r := &fakeRow{
		row: row{id: s.nextId, topic: topic, recipient: recipient, method: "m" + strconv.FormatInt(s.nextId, 10),
			createdAt: time.Now().UnixMilli(), initiator: 1},
		payload: []byte("{}"),
		state:   state,
	}
	if class, _ := userbox.TopicClassOf(topic); class == userbox.TopicInbox {
		r.payload = []byte(`{"inbox":true}`)
	}
	r.size = len(r.payload)
	s.rows[r.id] = r
	return r.id
}

func (s *fakeStore) get(id int64) (fakeRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return fakeRow{}, false
	}
	return *r, true
}

func (s *fakeStore) Lead(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lockFree {
		return false, nil
	}
	s.lockFree, s.leading, s.lostLock = false, true, false
	return true, nil
}

func (s *fakeStore) Leading(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leading && !s.lostLock, nil
}

func (s *fakeStore) Resign() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leading {
		s.leading, s.lockFree = false, true
	}
}

func (s *fakeStore) Pending(_ context.Context, topics []string, skip map[int64]bool, limit int) ([]row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []row
	for _, r := range s.rows {
		if r.state == userbox.EventPending && slices.Contains(topics, r.topic) && !skip[r.id] {
			out = append(out, r.row)
		}
	}
	slices.SortFunc(out, func(a, b row) int { return int(a.id - b.id) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeStore) Payloads(_ context.Context, ids []int64) (map[int64][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64][]byte{}
	for _, id := range ids {
		if r, ok := s.rows[id]; ok {
			out[id] = r.payload
		}
	}
	return out, nil
}

func (s *fakeStore) Finish(_ context.Context, f finish) error {
	if s.finishGate != nil {
		<-s.finishGate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishes++
	if s.finishErr != nil {
		return s.finishErr
	}
	for _, a := range f.published {
		if r, ok := s.rows[a.id]; ok && r.revision == a.revision {
			r.state, r.revision, r.partition, r.offset, r.dueAt = userbox.EventPublished, r.revision+1, a.partition, a.offset, a.dueAt
			r.published++
		}
	}
	for _, d := range f.deleted {
		if r, ok := s.rows[d.id]; ok && r.revision == d.revision {
			delete(s.rows, d.id)
		}
	}
	return nil
}

func (s *fakeStore) Dead(_ context.Context, r row, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.rows[r.id]; ok && x.revision == r.revision && x.state == userbox.EventPending {
		x.state, x.revision = userbox.EventDead, x.revision+1
	}
	return nil
}

func (s *fakeStore) ResetPublished(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.rows {
		if r.state == userbox.EventPublished {
			r.state, r.revision = userbox.EventPending, r.revision+1
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) DueForCheck(_ context.Context, now int64, limit int) ([]published, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []published
	for _, r := range s.rows {
		if r.state == userbox.EventPublished && r.dueAt <= now {
			out = append(out, published{id: r.id, revision: r.revision, topic: r.topic, partition: r.partition, offset: r.offset, redeliveries: r.redeliveries})
		}
	}
	slices.SortFunc(out, func(a, b published) int { return int(a.id - b.id) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeStore) Redeliver(_ context.Context, ref ref) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[ref.id]; ok && r.revision == ref.revision && r.state == userbox.EventPublished {
		r.state, r.revision, r.redeliveries = userbox.EventPending, r.revision+1, r.redeliveries+1
		return true, nil
	}
	return false, nil
}

func (s *fakeStore) Postpone(_ context.Context, ref ref, dueAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[ref.id]; ok && r.revision == ref.revision {
		r.dueAt = dueAt
	}
	return nil
}

func (s *fakeStore) DeadLanes(_ context.Context, now int64) ([]Lane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Lane
	for _, r := range s.rows {
		l := Lane{Topic: r.topic, Recipient: r.recipient}
		if r.state == userbox.EventDead && (now == 0 || r.dueAt <= now) && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *fakeStore) ReplayLane(_ context.Context, l Lane, auto bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.rows {
		if r.topic == l.Topic && r.recipient == l.Recipient && (r.state == userbox.EventDead || r.state == userbox.EventParked) {
			r.state, r.revision = userbox.EventPending, r.revision+1
			if auto {
				r.replays++
			} else {
				r.replays = 0
			}
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) Counts(context.Context) (counts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := counts{rows: map[userbox.EventState]int64{}}
	for _, r := range s.rows {
		c.rows[r.state]++
	}
	return c, nil
}

// verdict is what the fake broker does with a message.
type verdict struct {
	err    error
	silent bool // never answers
}

// fakeKafka is a broker with one partition log per topic and partition.
type fakeKafka struct {
	mu         sync.Mutex
	partitions map[string]int
	logs       map[string][]*sarama.ProducerMessage // topic/partition → messages
	accepted   []*sarama.ProducerMessage            // in the order taken
	decide     func(m *sarama.ProducerMessage) verdict
	producers  int
	dropped    int
	committed  map[string]map[int32]int64
	first      map[string]int64 // topic/partition → first offset
}

func newFakeKafka() *fakeKafka {
	return &fakeKafka{
		partitions: map[string]int{"Inbox-T": Partitions, "Sync-T": Partitions},
		logs:       map[string][]*sarama.ProducerMessage{},
		committed:  map[string]map[int32]int64{},
		first:      map[string]int64{},
	}
}

func logKey(topic string, partition int32) string {
	return topic + "/" + strconv.Itoa(int(partition))
}

func (k *fakeKafka) dial(brokers []string, clientID string) (kafkaConn, error) {
	return &fakeConn{k: k}, nil
}

func (k *fakeKafka) setDecide(f func(m *sarama.ProducerMessage) verdict) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.decide = f
}

// counts returns the producers made and dropped.
func (k *fakeKafka) counts() (made, dropped int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.producers, k.dropped
}

func (k *fakeKafka) taken() []*sarama.ProducerMessage {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.accepted)
}

type fakeConn struct {
	k *fakeKafka
	p *fakeProducer
}

func (c *fakeConn) Producer() (sarama.AsyncProducer, error) {
	if c.p == nil {
		c.p = newFakeProducer(c.k)
		c.k.mu.Lock()
		c.k.producers++
		c.k.mu.Unlock()
	}
	return c.p, nil
}

func (c *fakeConn) DropProducer() {
	if c.p != nil {
		c.p.AsyncClose()
		c.p = nil
		c.k.mu.Lock()
		c.k.dropped++
		c.k.mu.Unlock()
	}
}

func (c *fakeConn) Partitions(topics []string) (map[string]int, map[string]error, error) {
	c.k.mu.Lock()
	defer c.k.mu.Unlock()
	counts, errs := map[string]int{}, map[string]error{}
	for _, t := range topics {
		if n, ok := c.k.partitions[t]; ok {
			counts[t] = n
		} else {
			errs[t] = sarama.ErrUnknownTopicOrPartition
		}
	}
	return counts, errs, nil
}

func (c *fakeConn) Committed(group, topic string, partitions []int32) (map[int32]int64, error) {
	c.k.mu.Lock()
	defer c.k.mu.Unlock()
	out := map[int32]int64{}
	for _, p := range partitions {
		out[p] = -1
		if o, ok := c.k.committed[topic][p]; ok {
			out[p] = o
		}
	}
	return out, nil
}

func (c *fakeConn) Bounds(topic string, partition int32) (int64, int64, error) {
	c.k.mu.Lock()
	defer c.k.mu.Unlock()
	key := logKey(topic, partition)
	return c.k.first[key], int64(len(c.k.logs[key])), nil
}

func (c *fakeConn) Close() { c.DropProducer() }

// fakeProducer answers each message by the broker's verdict.
type fakeProducer struct {
	sarama.AsyncProducer
	k         *fakeKafka
	input     chan *sarama.ProducerMessage
	successes chan *sarama.ProducerMessage
	errors    chan *sarama.ProducerError
	closeOnce sync.Once
}

func newFakeProducer(k *fakeKafka) *fakeProducer {
	p := &fakeProducer{
		k:         k,
		input:     make(chan *sarama.ProducerMessage),
		successes: make(chan *sarama.ProducerMessage, 1024),
		errors:    make(chan *sarama.ProducerError, 1024),
	}
	go p.run()
	return p
}

func (p *fakeProducer) run() {
	for m := range p.input {
		var v verdict
		p.k.mu.Lock()
		decide := p.k.decide
		p.k.mu.Unlock()
		if decide != nil {
			v = decide(m)
		}
		switch {
		case v.silent:
		case v.err != nil:
			p.errors <- &sarama.ProducerError{Msg: m, Err: v.err}
		default:
			key, _ := m.Key.Encode()
			h := fnv.New32a()
			_, _ = h.Write(key)
			p.k.mu.Lock()
			m.Partition = int32(h.Sum32() % uint32(p.k.partitions[m.Topic]))
			lk := logKey(m.Topic, m.Partition)
			m.Offset = int64(len(p.k.logs[lk]))
			p.k.logs[lk] = append(p.k.logs[lk], m)
			p.k.accepted = append(p.k.accepted, m)
			p.k.mu.Unlock()
			p.successes <- m
		}
	}
}

func (p *fakeProducer) Input() chan<- *sarama.ProducerMessage     { return p.input }
func (p *fakeProducer) Successes() <-chan *sarama.ProducerMessage { return p.successes }
func (p *fakeProducer) Errors() <-chan *sarama.ProducerError      { return p.errors }
func (p *fakeProducer) AsyncClose()                               { p.closeOnce.Do(func() { close(p.input) }) }
func (p *fakeProducer) Close() error                              { p.AsyncClose(); return nil }

// testRelay is a relay over the fakes with its finisher running.
func testRelay(st *fakeStore, k *fakeKafka) *Relay {
	r := newRelay(st, k.dial, RelayConf{Groups: []GroupConf{{Topic: "Inbox-T", Group: "inbox"}}}, "test")
	r.roundTimeout = 200 * time.Millisecond
	r.jobs = make(chan finishJob, 16)
	go r.finisher()
	return r
}

// waitFinished waits until the finisher recorded every answer.
func (r *Relay) waitFinished() {
	for range 500 {
		if len(r.inflightSet()) == 0 && len(r.jobs) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

var errFake = errors.New("fake")
