package dao

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// blockingProducer stands for a Kafka client whose broker does not answer:
// a send returns only when released.
type blockingProducer struct {
	mu       sync.Mutex
	release  chan struct{}
	sent     int
	closed   bool
	closedAt int // sends finished when Close was called
}

func (p *blockingProducer) SendMessageV2(context.Context, string, string, []byte) (int32, int64, error) {
	<-p.release
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent++
	return 0, 0, nil
}

func (p *blockingProducer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed, p.closedAt = true, p.sent
	return nil
}

// A send that the broker does not answer ends with the event's context,
// though the producer takes none; Close waits for it, then refuses events.
func TestKafkaPublisherDeadline(t *testing.T) {
	pr := &blockingProducer{release: make(chan struct{})}
	p := newKafkaPublisher(map[string]producer{"T": pr})
	ev := userbox.Event{Topic: "T", Recipient: 1, Method: "m", Data: []byte("{}")}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.Publish(ctx, ev)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Publish: %v, want the deadline", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Publish returned after %v", d)
	}

	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	time.Sleep(50 * time.Millisecond)
	close(pr.release) // the broker answers the pending send
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	if !pr.closed || pr.closedAt != 1 {
		t.Errorf("closed %v after %d sends, want after the pending one", pr.closed, pr.closedAt)
	}
	if err = p.Publish(context.Background(), ev); !errors.Is(err, errPublisherClosed) {
		t.Errorf("Publish after Close: %v", err)
	}
	if err = p.Publish(context.Background(), userbox.Event{Topic: "other"}); err == nil {
		t.Error("Publish to a topic without a producer succeeded")
	}
}
