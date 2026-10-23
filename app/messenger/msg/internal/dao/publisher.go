package dao

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// producer sends messages to one Kafka topic (marmota mq.Producer). Its send
// takes no deadline: the Kafka client waits for the broker by its own
// network timeouts and retries.
type producer interface {
	SendMessageV2(ctx context.Context, method, key string, value []byte) (partition int32, offset int64, err error)
	Close() error
}

// errPublisherClosed: an event published after Close.
var errPublisherClosed = errors.New("msg: the producers are closed")

// closeWait is how long Close waits for the sends still running before it
// closes the producers.
const closeWait = userbox.EventTimeout

// kafkaPublisher sends events with the producer of their topic, the
// recipient as the key.
type kafkaPublisher struct {
	producers map[string]producer

	mu      sync.Mutex
	closed  bool
	sending sync.WaitGroup
}

func newKafkaPublisher(producers map[string]producer) *kafkaPublisher {
	return &kafkaPublisher{producers: producers}
}

// Publish sends ev and waits for the broker's answer while ctx lives, so the
// limits of userbox.Publish hold. The producer cannot be interrupted: the
// send goes on in the background after ctx ends, and the event may still
// reach the topic after it was counted as not published.
func (p *kafkaPublisher) Publish(ctx context.Context, ev userbox.Event) error {
	pr, ok := p.producers[ev.Topic]
	if !ok {
		return fmt.Errorf("msg: no producer for topic %q", ev.Topic)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errPublisherClosed
	}
	p.sending.Add(1)
	p.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		defer p.sending.Done()
		_, _, err := pr.SendMessageV2(context.WithoutCancel(ctx), ev.Method, strconv.FormatInt(ev.Recipient, 10), ev.Data)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("msg: event %s to user %d not confirmed: %w", ev.Method, ev.Recipient, ctx.Err())
	}
}

// Close refuses new events, waits up to closeWait for the sends still
// running, then flushes and closes the producers.
func (p *kafkaPublisher) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.sending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeWait):
		logx.Errorf("msg: Kafka sends still running after %v, closing the producers", closeWait)
	}
	var errs []error
	for _, pr := range p.producers {
		errs = append(errs, pr.Close())
	}
	return errors.Join(errs...)
}
