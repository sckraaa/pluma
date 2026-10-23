package mqconsumer

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

type fakeSession struct {
	sarama.ConsumerGroupSession
	ctx context.Context

	mu     sync.Mutex
	marked []int64
}

func (s *fakeSession) Context() context.Context { return s.ctx }

func (s *fakeSession) MarkMessage(msg *sarama.ConsumerMessage, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = append(s.marked, msg.Offset)
}

func (s *fakeSession) offsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.marked)
}

type fakeClaim struct {
	sarama.ConsumerGroupClaim
	msgs chan *sarama.ConsumerMessage
}

func (c *fakeClaim) Topic() string                            { return "T" }
func (c *fakeClaim) Partition() int32                         { return 3 }
func (c *fakeClaim) Messages() <-chan *sarama.ConsumerMessage { return c.msgs }

func claimOf(n int) *fakeClaim {
	c := &fakeClaim{msgs: make(chan *sarama.ConsumerMessage, n)}
	for i := range n {
		c.msgs <- &sarama.ConsumerMessage{Topic: "T", Partition: 3, Offset: int64(i), Key: []byte("7"),
			Headers: []*sarama.RecordHeader{{Key: []byte("event-id"), Value: []byte{byte('0' + i)}}}}
	}
	close(c.msgs)
	return c
}

var (
	errTransient     = errors.New("connection refused")
	errDeterministic = errors.New("bad data")
)

// script is a handler that answers each attempt of each offset by a list.
type script struct {
	mu       sync.Mutex
	answers  map[int64][]error
	attempts map[int64]int
}

func (s *script) Handle(_ context.Context, m *Message) (Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.attempts[m.Offset]
	s.attempts[m.Offset]++
	if list := s.answers[m.Offset]; n < len(list) {
		if list[n] != nil && list[n].Error() == "panic" {
			panic("handler bug")
		}
		return "", list[n]
	}
	return "applied", nil
}

func (s *script) count(offset int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts[offset]
}

type deadRecord struct {
	offset   int64
	cause    error
	attempts int
}

func newTestConsumer(h Handler, dead func(m *Message, cause error, attempts int) error) *Consumer {
	return New(Conf{Topic: "T", Group: "g"}, "test", h, Policy{
		Deterministic: func(err error) bool { return errors.Is(err, errDeterministic) },
		Dead: func(_ context.Context, m *Message, cause error, attempts int) error {
			return dead(m, cause, attempts)
		},
	})
}

func TestConsumerMarksOnlyDone(t *testing.T) {
	h := &script{answers: map[int64][]error{1: {errTransient, errTransient}}, attempts: map[int64]int{}}
	c := newTestConsumer(h, func(*Message, error, int) error { t.Error("dead"); return nil })
	sess := &fakeSession{ctx: context.Background()}
	if err := c.consumeClaim(sess, claimOf(3)); err != nil {
		t.Fatal(err)
	}
	if got := sess.offsets(); !slices.Equal(got, []int64{0, 1, 2}) {
		t.Errorf("marked %v", got)
	}
	if h.count(1) != 3 {
		t.Errorf("%d attempts of the failing message", h.count(1))
	}
}

// A temporary error is tried again until the session ends; nothing after the
// failing message is handled or marked.
func TestConsumerTransientUntilSessionEnds(t *testing.T) {
	failing := make([]error, 1000)
	for i := range failing {
		failing[i] = errTransient
	}
	h := &script{answers: map[int64][]error{1: failing}, attempts: map[int64]int{}}
	c := newTestConsumer(h, func(*Message, error, int) error { t.Error("dead"); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	sess := &fakeSession{ctx: ctx}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.consumeClaim(sess, claimOf(3))
	}()
	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the claim did not end with the session")
	}
	if got := sess.offsets(); !slices.Equal(got, []int64{0}) {
		t.Errorf("marked %v", got)
	}
	if n := h.count(1); n < 3 {
		t.Errorf("%d attempts in 500 ms", n)
	}
	if h.count(2) != 0 {
		t.Error("handled the next message")
	}
}

// A deterministic error: three attempts, then dead, then marked.
func TestConsumerDeterministic(t *testing.T) {
	h := &script{answers: map[int64][]error{0: {errDeterministic, errDeterministic, errDeterministic, errDeterministic}}, attempts: map[int64]int{}}
	var dead []deadRecord
	c := newTestConsumer(h, func(m *Message, cause error, attempts int) error {
		dead = append(dead, deadRecord{m.Offset, cause, attempts})
		return nil
	})
	sess := &fakeSession{ctx: context.Background()}
	start := time.Now()
	_ = c.consumeClaim(sess, claimOf(2))
	if h.count(0) != DefaultAttempts {
		t.Errorf("%d attempts", h.count(0))
	}
	if len(dead) != 1 || dead[0].offset != 0 || dead[0].attempts != DefaultAttempts || !errors.Is(dead[0].cause, errDeterministic) {
		t.Errorf("dead %+v", dead)
	}
	if got := sess.offsets(); !slices.Equal(got, []int64{0, 1}) {
		t.Errorf("marked %v", got)
	}
	if d := time.Since(start); d < 300*time.Millisecond || d > 2*time.Second {
		t.Errorf("attempts took %v, want the pauses of 100 and 200 ms", d)
	}
}

// Recording the dead event fails: a temporary error, the message is not
// marked and is handled again from its first attempt.
func TestConsumerDeadFails(t *testing.T) {
	answers := make([]error, 6)
	for i := range answers {
		answers[i] = errDeterministic
	}
	h := &script{answers: map[int64][]error{0: answers}, attempts: map[int64]int{}}
	calls := 0
	c := newTestConsumer(h, func(*Message, error, int) error {
		calls++
		if calls == 1 {
			return errTransient
		}
		return nil
	})
	sess := &fakeSession{ctx: context.Background()}
	_ = c.consumeClaim(sess, claimOf(1))
	if calls != 2 || h.count(0) != 2*DefaultAttempts {
		t.Errorf("%d dead records, %d attempts", calls, h.count(0))
	}
	if got := sess.offsets(); !slices.Equal(got, []int64{0}) {
		t.Errorf("marked %v", got)
	}
}

// A panic of the handler is deterministic.
func TestConsumerPanic(t *testing.T) {
	boom := errors.New("panic")
	h := &script{answers: map[int64][]error{0: {boom, boom, boom}}, attempts: map[int64]int{}}
	var cause error
	c := newTestConsumer(h, func(_ *Message, err error, _ int) error { cause = err; return nil })
	sess := &fakeSession{ctx: context.Background()}
	_ = c.consumeClaim(sess, claimOf(1))
	var pe *PanicError
	if !errors.As(cause, &pe) {
		t.Fatalf("dead with %v", cause)
	}
	if got := sess.offsets(); !slices.Equal(got, []int64{0}) {
		t.Errorf("marked %v", got)
	}
}

// The attempt that is running when the session ends finishes and is marked:
// a box transaction that started commits.
func TestConsumerFinishesAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	h := HandlerFunc(func(hctx context.Context, m *Message) (Outcome, error) {
		if m.Offset == 0 {
			close(started)
			time.Sleep(200 * time.Millisecond)
			if hctx.Err() != nil {
				return "", hctx.Err()
			}
		}
		return "applied", nil
	})
	c := New(Conf{Topic: "T", Group: "g"}, "test", h, Policy{})
	sess := &fakeSession{ctx: ctx}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.consumeClaim(sess, claimOf(2))
	}()
	<-started
	cancel()
	<-done
	if got := sess.offsets(); !slices.Equal(got, []int64{0}) {
		t.Errorf("marked %v", got)
	}
}

func TestToMessage(t *testing.T) {
	ts := time.UnixMilli(1_700_000_000_000)
	m := toMessage(&sarama.ConsumerMessage{Topic: "T", Partition: 2, Offset: 9, Key: []byte("7"), Value: []byte("{}"), Timestamp: ts,
		Headers: []*sarama.RecordHeader{{Key: []byte("method"), Value: []byte("m")}, nil}})
	if m.Header("method") != "m" || m.Header("none") != "" || string(m.Key) != "7" || !m.Timestamp.Equal(ts) || m.Offset != 9 {
		t.Errorf("%+v", m)
	}
}

func TestGroupConfig(t *testing.T) {
	c := GroupConfig("msg-host")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Consumer.Offsets.Initial != sarama.OffsetOldest || c.Consumer.Group.InstanceId != "msg-host" {
		t.Errorf("offsets from %d, instance %q", c.Consumer.Offsets.Initial, c.Consumer.Group.InstanceId)
	}
}
