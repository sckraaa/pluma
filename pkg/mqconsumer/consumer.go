// Package mqconsumer reads a Kafka topic as a consumer group and hands each
// message to a handler that says when it is done (ADR 0012, pp. 5 and 7).
//
// The offset of a message is marked only once its handler returned nil, or
// once the policy recorded it as dead: a failure never moves the offset,
// and after a crash the messages not done are read again. A partition is
// handled in order, one message at a time. Errors the policy calls
// deterministic are tried a few times and then given to Policy.Dead;
// every other error is temporary and tried again without limit, with a
// growing pause, while the partition waits (mq_consumer_stuck_seconds).
//
// Nothing depends on Kafka to start: the group connects in the background,
// with a growing pause between failures. The group reads from the oldest
// offset when it has none, and is a static member (KIP-345): a restarted
// instance gets its partitions back without waiting for the coordinator to
// drop the old one.
package mqconsumer

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/IBM/sarama"

	"github.com/zeromicro/go-zero/core/logx"
)

// Conf is a consumer's part of a service config.
type Conf struct {
	Brokers []string
	Topic   string
	Group   string
	// InstanceId is the static member id of this instance in the group;
	// by default "<service>-<hostname>".
	InstanceId string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Settings of the group (ADR 0012, p. 5).
const (
	sessionTimeout   = 10 * time.Second
	heartbeat        = time.Second
	rebalanceTimeout = 30 * time.Second
	commitInterval   = time.Second

	connectMin   = time.Second
	connectMax   = 30 * time.Second
	transientMin = 100 * time.Millisecond
	transientMax = 30 * time.Second

	// DefaultAttempts of a deterministic error before Dead.
	DefaultAttempts = 3
)

// deterministicPauses are the pauses between the attempts of a
// deterministic error.
var deterministicPauses = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

// Version is the protocol version of the group: the broker is 3.9, static
// membership needs 2.3.
var Version = sarama.V3_6_0_0

// Message is a Kafka record.
type Message struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       []byte
	Value     []byte
	Headers   map[string]string
	Timestamp time.Time
}

// Header returns a header's value, "" without it.
func (m *Message) Header(name string) string { return m.Headers[name] }

// Outcome names how a message ended, for mq_consumer_events_total.
type Outcome string

// Outcomes of the consumer itself.
const (
	OutcomeDead    Outcome = "dead"
	OutcomeSkipped Outcome = "skipped"
)

// Handler handles one message. nil: done, the offset may pass it.
type Handler interface {
	Handle(ctx context.Context, m *Message) (Outcome, error)
}

// HandlerFunc is a Handler.
type HandlerFunc func(ctx context.Context, m *Message) (Outcome, error)

func (f HandlerFunc) Handle(ctx context.Context, m *Message) (Outcome, error) { return f(ctx, m) }

// Policy says what errors of the handler mean.
type Policy struct {
	// Deterministic tells whether an error comes back the same on every
	// attempt. nil: no error does.
	Deterministic func(error) bool
	// Attempts of a deterministic error before Dead; DefaultAttempts if 0.
	Attempts int
	// Dead records a message that will not succeed; nil marks it done. An
	// error of Dead is temporary: the message is handled again after a
	// pause, from its first attempt.
	Dead func(ctx context.Context, m *Message, cause error, attempts int) error
}

// PanicError is a panic of the handler: deterministic.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("mqconsumer: handler panic: %v", e.Value) }

func (p Policy) deterministic(err error) bool {
	var pe *PanicError
	if errors.As(err, &pe) {
		return true
	}
	return p.Deterministic != nil && p.Deterministic(err)
}

func (p Policy) attempts() int {
	if p.Attempts > 0 {
		return p.Attempts
	}
	return DefaultAttempts
}

// Consumer reads one topic as a consumer group.
type Consumer struct {
	conf    Conf
	handler Handler
	policy  Policy
	log     logx.Logger

	cancel context.CancelFunc
	done   chan struct{}

	afterHandle func(m *Message)
}

// SetAfterHandle makes f run after a message is done and before its
// offset is marked: the point where a crash leaves an applied message to
// be read again. For fault injection in tests; call before Start.
func (c *Consumer) SetAfterHandle(f func(m *Message)) { c.afterHandle = f }

// New returns a consumer of conf.Topic for service (the default instance
// id is "<service>-<hostname>").
func New(conf Conf, service string, h Handler, p Policy) *Consumer {
	if conf.InstanceId == "" {
		host, _ := os.Hostname()
		conf.InstanceId = service + "-" + host
	}
	return &Consumer{conf: conf, handler: h, policy: p, log: logx.WithContext(context.Background())}
}

// GroupConfig is the config of the consumer group.
func GroupConfig(instanceId string) *sarama.Config {
	c := sarama.NewConfig()
	c.ClientID = instanceId
	c.Version = Version
	c.Consumer.Offsets.Initial = sarama.OffsetOldest
	c.Consumer.Offsets.AutoCommit.Enable = true
	c.Consumer.Offsets.AutoCommit.Interval = commitInterval
	c.Consumer.Group.InstanceId = instanceId
	c.Consumer.Group.Session.Timeout = sessionTimeout
	c.Consumer.Group.Heartbeat.Interval = heartbeat
	c.Consumer.Group.Rebalance.Timeout = rebalanceTimeout
	c.Consumer.Return.Errors = true
	c.Metadata.AllowAutoTopicCreation = false
	c.Net.DialTimeout = 3 * time.Second
	return c
}

// Start reads in the background until Stop.
func (c *Consumer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		c.run(ctx)
	}()
}

// Stop lets the message being handled finish, marks it, commits the
// offsets and closes the group. A static member does not leave the group:
// its partitions wait for it for the session timeout.
func (c *Consumer) Stop() {
	if c.cancel == nil {
		return
	}
	c.cancel()
	<-c.done
}

func (c *Consumer) run(ctx context.Context) {
	cfg := GroupConfig(c.conf.InstanceId)
	gap := time.Duration(0)
	wait := func() bool {
		gap = min(max(2*gap, connectMin), connectMax)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(gap):
			return true
		}
	}
	for ctx.Err() == nil {
		group, err := sarama.NewConsumerGroup(c.conf.Brokers, c.conf.Group, cfg)
		if err != nil {
			c.log.Errorf("mqconsumer: %s: connect (next try in %v): %v", c.conf.Group, min(max(2*gap, connectMin), connectMax), err)
			if !wait() {
				return
			}
			continue
		}
		errs := make(chan struct{})
		go func() {
			defer close(errs)
			for err := range group.Errors() {
				c.log.Errorf("mqconsumer: %s: %v", c.conf.Group, err)
			}
		}()
		for ctx.Err() == nil {
			err = group.Consume(ctx, []string{c.conf.Topic}, groupHandler{c})
			if errors.Is(err, sarama.ErrClosedConsumerGroup) {
				break
			}
			if err != nil {
				c.log.Errorf("mqconsumer: %s: consume %s: %v", c.conf.Group, c.conf.Topic, err)
				if !wait() {
					break
				}
				continue
			}
			gap = 0
		}
		if err = group.Close(); err != nil {
			c.log.Errorf("mqconsumer: %s: close: %v", c.conf.Group, err)
		}
		<-errs
		metricAssigned.Set(0, c.conf.Group)
	}
}

// groupHandler is the sarama side of the consumer.
type groupHandler struct{ c *Consumer }

func (h groupHandler) Setup(sess sarama.ConsumerGroupSession) error {
	n := 0
	for _, p := range sess.Claims() {
		n += len(p)
	}
	metricAssigned.Set(float64(n), h.c.conf.Group)
	return nil
}

func (h groupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	metricAssigned.Set(0, h.c.conf.Group)
	return nil
}

func (h groupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	return h.c.consumeClaim(sess, claim)
}

// consumeClaim handles a partition's messages in order. It returns when the
// session ends, leaving the message being retried unmarked.
func (c *Consumer) consumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	partition := strconv.Itoa(int(claim.Partition()))
	defer metricStuck.Set(0, claim.Topic(), partition)
	for {
		select {
		case <-sess.Context().Done():
			return nil
		case msg, ok := <-claim.Messages():
			// A message that arrives as the session ends goes to the next
			// owner of the partition.
			if !ok || sess.Context().Err() != nil {
				return nil
			}
			if !c.process(sess, msg, partition) {
				return nil
			}
		}
	}
}

// process handles one message until it is done or the session ends; it
// tells whether it is done.
func (c *Consumer) process(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, partition string) bool {
	m := toMessage(msg)
	// The attempt itself is not cut by the end of the session: a box
	// transaction that started finishes, and its event is marked.
	hctx := context.WithoutCancel(sess.Context())
	var (
		failedSince time.Time
		pause       = transientMin
		det         int
	)
	sleep := func(d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-sess.Context().Done():
			return false
		case <-t.C:
			return true
		}
	}
	done := func(outcome Outcome) bool {
		if c.afterHandle != nil {
			c.afterHandle(m)
		}
		sess.MarkMessage(msg, "")
		metricEvents.Inc(m.Topic, string(outcome))
		if !m.Timestamp.IsZero() {
			metricAge.ObserveFloat(time.Since(m.Timestamp).Seconds(), m.Topic)
		}
		metricStuck.Set(0, m.Topic, partition)
		return true
	}
	for {
		outcome, err := c.handle(hctx, m)
		if err == nil {
			return done(outcome)
		}
		if failedSince.IsZero() {
			failedSince = time.Now()
		}
		if sess.Context().Err() != nil {
			return false
		}
		if c.policy.deterministic(err) {
			det++
			metricRetries.Inc(m.Topic, "deterministic")
			if det < c.policy.attempts() {
				c.log.Errorf("mqconsumer: %s %d/%d: attempt %d: %v", m.Topic, m.Partition, m.Offset, det, err)
				if !sleep(deterministicPauses[min(det-1, len(deterministicPauses)-1)]) {
					return false
				}
				continue
			}
			if c.policy.Dead != nil {
				derr := c.policy.Dead(hctx, m, err, det)
				if derr == nil {
					return done(OutcomeDead)
				}
				c.log.Errorf("mqconsumer: %s %d/%d: record as dead: %v", m.Topic, m.Partition, m.Offset, derr)
			}
			det = 0
		} else {
			metricRetries.Inc(m.Topic, "transient")
			c.log.Errorf("mqconsumer: %s %d/%d: temporary error, again in %v: %v", m.Topic, m.Partition, m.Offset, pause, err)
		}
		metricStuck.Set(time.Since(failedSince).Seconds(), m.Topic, partition)
		if !sleep(pause + rand.N(pause/5+1)) { //nolint:gosec // spreading retries, not a secret
			return false
		}
		pause = min(2*pause, transientMax)
	}
}

// handle runs the handler, turning a panic into a PanicError.
func (c *Consumer) handle(ctx context.Context, m *Message) (outcome Outcome, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &PanicError{Value: v, Stack: debug.Stack()}
			c.log.Errorf("mqconsumer: %s %d/%d: %v\n%s", m.Topic, m.Partition, m.Offset, err, err.(*PanicError).Stack)
		}
	}()
	return c.handler.Handle(ctx, m)
}

func toMessage(msg *sarama.ConsumerMessage) *Message {
	m := &Message{
		Topic:     msg.Topic,
		Partition: msg.Partition,
		Offset:    msg.Offset,
		Key:       msg.Key,
		Value:     msg.Value,
		Timestamp: msg.Timestamp,
		Headers:   make(map[string]string, len(msg.Headers)),
	}
	for _, h := range msg.Headers {
		if h != nil {
			m.Headers[string(h.Key)] = string(h.Value)
		}
	}
	return m
}
