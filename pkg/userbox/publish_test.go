package userbox

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestPublish(t *testing.T) {
	events := []Event{
		{Recipient: 1, Method: "a"},
		{Recipient: 2, Method: "b"},
		{Recipient: 1, Method: "c"},
		{Recipient: 3, Method: "d"},
		{Recipient: 2, Method: "e"},
	}
	errDown := errors.New("kafka down")

	// The request is gone by the time the events go out: they are sent
	// anyway, each with its own limit.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var sent []string
	failures := Publish(ctx, events, func(ctx context.Context, ev Event) error {
		if ctx.Err() != nil {
			t.Errorf("event %s: context already done: %v", ev.Method, ctx.Err())
		}
		if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > EventTimeout {
			t.Errorf("event %s: deadline %v, want within %v", ev.Method, dl, EventTimeout)
		}
		if ev.Method == "a" {
			return errDown
		}
		sent = append(sent, ev.Method)
		return nil
	})

	// After "a" to user 1 failed, "c" to user 1 is held back; the others go.
	if want := []string{"b", "d", "e"}; !slices.Equal(sent, want) {
		t.Errorf("sent %v, want %v", sent, want)
	}
	if len(failures) != 2 ||
		failures[0].Event.Method != "a" || !errors.Is(failures[0].Err, errDown) ||
		failures[1].Event.Method != "c" || !errors.Is(failures[1].Err, ErrSkipped) {
		t.Errorf("failures %+v", failures)
	}
}

func TestPublishNothing(t *testing.T) {
	if f := Publish(context.Background(), nil, func(context.Context, Event) error {
		t.Error("send called")
		return nil
	}); len(f) != 0 {
		t.Errorf("failures %+v", f)
	}
}

// A send that runs out of its time takes the rest of its topic with it: the
// broker does not answer, and each later event would wait as long. Other
// topics still go.
func TestPublishTopicTimeout(t *testing.T) {
	events := []Event{
		{Topic: "inbox", Recipient: 1, Method: "a"},
		{Topic: "sync", Recipient: 9, Method: "b"},
		{Topic: "inbox", Recipient: 2, Method: "c"},
		{Topic: "sync", Recipient: 9, Method: "d"},
		{Topic: "inbox", Recipient: 3, Method: "e"},
	}
	var sent []string
	start := time.Now()
	failures := publish(context.Background(), events, func(ctx context.Context, ev Event) error {
		if ev.Topic == "inbox" {
			<-ctx.Done() // the broker does not answer
			return ctx.Err()
		}
		sent = append(sent, ev.Method)
		return nil
	}, 50*time.Millisecond, time.Minute)
	if d := time.Since(start); d > time.Second {
		t.Errorf("publication took %v: one timeout per topic expected", d)
	}
	if want := []string{"b", "d"}; !slices.Equal(sent, want) {
		t.Errorf("sent %v, want %v", sent, want)
	}
	if len(failures) != 3 ||
		!errors.Is(failures[0].Err, context.DeadlineExceeded) ||
		!errors.Is(failures[1].Err, ErrTopicTimedOut) || failures[1].Event.Method != "c" ||
		!errors.Is(failures[2].Err, ErrTopicTimedOut) || failures[2].Event.Method != "e" {
		t.Errorf("failures %+v", failures)
	}
}

// All the events of a transaction together get PublishTimeout, however slow
// each successful send is.
func TestPublishTimeout(t *testing.T) {
	var events []Event
	for i := range 20 {
		events = append(events, Event{Topic: "inbox", Recipient: int64(i + 1), Method: "m"})
	}
	sent := 0
	start := time.Now()
	failures := publish(context.Background(), events, func(ctx context.Context, ev Event) error {
		time.Sleep(20 * time.Millisecond) // slow, but within the event's time
		sent++
		return nil
	}, time.Second, 100*time.Millisecond)
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("publication took %v, limit 100ms", d)
	}
	if sent == 0 || sent+len(failures) != len(events) {
		t.Fatalf("sent %d, failed %d of %d", sent, len(failures), len(events))
	}
	for _, f := range failures {
		if !errors.Is(f.Err, ErrPublishTimeout) {
			t.Errorf("failure %+v", f)
		}
	}
}
