package userbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func init() {
	for topic, class := range map[string]TopicClass{"Inbox-T": TopicInbox, "Sync-T": TopicPush} {
		if err := RegisterTopic(topic, class); err != nil {
			panic(err)
		}
	}
}

func TestRegisterTopic(t *testing.T) {
	if err := RegisterTopic("Sync-T", TopicPush); err != nil {
		t.Errorf("registering a topic again: %v", err)
	}
	if err := RegisterTopic("Sync-T", TopicInbox); err == nil {
		t.Error("a topic changed its class")
	}
	if err := RegisterTopic("", TopicPush); err == nil {
		t.Error("an empty topic registered")
	}
	if c, ok := TopicClassOf("Inbox-T"); !ok || c != TopicInbox {
		t.Errorf("Inbox-T is %v, %v", c, ok)
	}
}

// An event that cannot be written rolls the transaction back: nothing of it
// is committed, and the error says why.
func TestEmitRefusesInvalidEvents(t *testing.T) {
	for _, c := range []struct {
		name string
		ev   Event
	}{
		{"unknown topic", Event{Topic: "Nope-T", Recipient: 2, Method: "m", Data: []byte("{}")}},
		{"no recipient", Event{Topic: "Sync-T", Recipient: 0, Method: "m", Data: []byte("{}")}},
		{"no method", Event{Topic: "Sync-T", Recipient: 2, Data: []byte("{}")}},
		{"too large", Event{Topic: "Sync-T", Recipient: 2, Method: "m", Data: make([]byte, MaxEventBytes+1)}},
		{"inbox event to its initiator", Event{Topic: "Inbox-T", Recipient: 7, Method: "m", Data: []byte("{}")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newFakeServer()
			_, err := Run(context.Background(), s.open(t), 7, func(b *Box) error {
				if err := sendMessage(b); err != nil {
					return err
				}
				b.Emit(c.ev)
				return nil
			})
			if !errors.Is(err, ErrInvalidEvent) || !Deterministic(err) {
				t.Fatalf("error %v, want %v", err, ErrInvalidEvent)
			}
			if _, ok := s.state(7); ok || len(s.events()) != 0 || s.count("commit") != 0 {
				t.Errorf("something was committed: %v", s.statements())
			}
		})
	}
	// A push to the initiator itself is fine: its other devices.
	s := newFakeServer()
	if _, err := Run(context.Background(), s.open(t), 7, func(b *Box) error {
		b.Emit(Event{Topic: "Sync-T", Recipient: 7, Method: "m", Data: []byte("{}")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A broadcast to 200 recipients with large events goes in several INSERTs,
// none over 4 MiB of payloads, the rows in the order emitted.
func TestEmitSplitsStatements(t *testing.T) {
	s := newFakeServer()
	const n = 200
	payload := func(i int) []byte {
		return bytes.Repeat([]byte{byte('a' + i%26)}, 100<<10+i) // ~100 KiB each, ~20 MiB together
	}
	res, err := Run(context.Background(), s.open(t), 7, func(b *Box) error {
		for i := range n {
			b.Emit(Event{Topic: "Inbox-T", Recipient: int64(100 + i), Method: fmt.Sprint(i), Data: payload(i)})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Emitted != n {
		t.Errorf("emitted %d", res.Emitted)
	}
	events := s.events()
	if len(events) != n {
		t.Fatalf("%d rows", len(events))
	}
	for i, e := range events {
		if e.method != fmt.Sprint(i) || e.recipient != int64(100+i) || e.initiator != 7 || !bytes.Equal(e.payload, payload(i)) || e.createdAt == 0 {
			t.Fatalf("row %d: %d %s to %d from %d", i, e.id, e.method, e.recipient, e.initiator)
		}
	}
	s.mu.Lock()
	inserts := slices.Clone(s.inserts)
	s.mu.Unlock()
	if len(inserts) < 5 {
		t.Errorf("%d statements for ~20 MiB", len(inserts))
	}
	byId := map[int64]fakeEvent{}
	for _, e := range events {
		byId[e.id] = e
	}
	for _, ids := range inserts {
		size := 0
		for _, id := range ids {
			size += len(byId[id].payload)
		}
		if size > maxStatementPayload {
			t.Errorf("a statement of %d rows carries %d bytes", len(ids), size)
		}
	}
}

// takeEvent runs a transaction of the recipient that takes event id and,
// when it may, applies it with apply.
func takeEvent(t *testing.T, s *fakeServer, recipient, id int64, apply func(b *Box) error) (TakeOutcome, error) {
	t.Helper()
	var outcome TakeOutcome
	res, err := Run(context.Background(), s.open(t), recipient, func(b *Box) error {
		var err error
		if outcome, err = b.TakeEvent(id); err != nil || outcome != TakeApply {
			return err
		}
		return apply(b)
	})
	if err == nil && res.Taken != outcome {
		t.Errorf("result says %v, TakeEvent %v", res.Taken, outcome)
	}
	return outcome, err
}

func inboxRow(recipient int64, state EventState) fakeEvent {
	return fakeEvent{topic: "Inbox-T", recipient: recipient, initiator: 1, method: "m", payload: []byte("{}"), state: state}
}

func TestTakeEvent(t *testing.T) {
	finish := func(b *Box) error { return b.FinishEvent() }

	t.Run("apply", func(t *testing.T) {
		s := newFakeServer()
		other := s.addEvent(inboxRow(8, EventPublished)) // another recipient's lane
		id := s.addEvent(inboxRow(7, EventPublished))
		outcome, err := takeEvent(t, s, 7, id, func(b *Box) error {
			b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
			return b.FinishEvent()
		})
		if err != nil || outcome != TakeApply {
			t.Fatalf("%v, %v", outcome, err)
		}
		if ev := s.events(); len(ev) != 1 || ev[0].id != other {
			t.Errorf("rows %+v: want the event's row deleted", ev)
		}
		// A copy delivered again finds nothing.
		outcome, err = takeEvent(t, s, 7, id, finish)
		if err != nil || outcome != TakeDuplicate {
			t.Fatalf("again: %v, %v", outcome, err)
		}
		if st, _ := s.state(7); st.Pts != BasePts+1 {
			t.Errorf("applied twice: pts %d", st.Pts)
		}
	})

	t.Run("not finished", func(t *testing.T) {
		s := newFakeServer()
		id := s.addEvent(inboxRow(7, EventPublished))
		_, err := takeEvent(t, s, 7, id, func(b *Box) error {
			b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
			return nil
		})
		if !errors.Is(err, ErrEventNotFinished) || !Deterministic(err) {
			t.Fatalf("error %v, want %v", err, ErrEventNotFinished)
		}
		if _, ok := s.state(7); ok || len(s.events()) != 1 {
			t.Error("committed without finishing the event")
		}
	})

	t.Run("kept", func(t *testing.T) {
		s := newFakeServer()
		id := s.addEvent(inboxRow(7, EventPublished))
		outcome, err := takeEvent(t, s, 7, id, func(b *Box) error {
			b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
			b.KeepEvent()
			return nil
		})
		if err != nil || outcome != TakeApply {
			t.Fatalf("%v, %v", outcome, err)
		}
		if ev := s.events(); len(ev) != 1 {
			t.Errorf("a kept event's row is gone")
		}
		if st, _ := s.state(7); st.Pts != BasePts+1 {
			t.Errorf("the portion is not committed")
		}
	})

	t.Run("someone else's", func(t *testing.T) {
		s := newFakeServer()
		id := s.addEvent(inboxRow(8, EventPublished))
		if _, err := takeEvent(t, s, 7, id, finish); !errors.Is(err, ErrMisuse) {
			t.Fatalf("error %v", err)
		}
		push := s.addEvent(fakeEvent{topic: "Sync-T", recipient: 7, method: "m"})
		if _, err := takeEvent(t, s, 7, push, finish); !errors.Is(err, ErrMisuse) {
			t.Fatalf("a push taken: %v", err)
		}
	})

	t.Run("waiting", func(t *testing.T) {
		for _, st := range []EventState{EventDead, EventParked} {
			s := newFakeServer()
			id := s.addEvent(inboxRow(7, st))
			outcome, err := takeEvent(t, s, 7, id, finish)
			if err != nil || outcome != TakeWaiting {
				t.Fatalf("%v: %v, %v", st, outcome, err)
			}
			if ev := s.events(); ev[0].state != st || ev[0].revision != 0 {
				t.Errorf("%v: row changed %+v", st, ev[0])
			}
		}
	})

	t.Run("deferred", func(t *testing.T) {
		s := newFakeServer()
		early := s.addEvent(inboxRow(7, EventPublished))
		pending := s.addEvent(inboxRow(7, EventPending))
		late := s.addEvent(inboxRow(7, EventPublished))
		after := s.addEvent(inboxRow(7, EventPublished)) // later in the lane: untouched
		outcome, err := takeEvent(t, s, 7, late, finish)
		if err != nil || outcome != TakeDeferred {
			t.Fatalf("%v, %v", outcome, err)
		}
		want := map[int64][3]int{ // state, revision, deferrals
			early:   {int(EventPending), 1, 0},
			pending: {int(EventPending), 0, 0},
			late:    {int(EventPending), 1, 1},
			after:   {int(EventPublished), 0, 0},
		}
		for _, e := range s.events() {
			if got := [3]int{int(e.state), e.revision, e.deferrals}; got != want[e.id] {
				t.Errorf("row %d: state, revision, deferrals %v, want %v", e.id, got, want[e.id])
			}
		}
		if _, ok := s.state(7); !ok {
			t.Error("the deferral is not committed")
		}
	})

	t.Run("parked behind a dead event", func(t *testing.T) {
		for _, st := range []EventState{EventDead, EventParked} {
			s := newFakeServer()
			s.addEvent(inboxRow(7, st))
			id := s.addEvent(inboxRow(7, EventPublished))
			outcome, err := takeEvent(t, s, 7, id, finish)
			if err != nil || outcome != TakeParked {
				t.Fatalf("%v, %v", outcome, err)
			}
			if e := s.events()[1]; e.state != EventParked || e.revision != 1 {
				t.Errorf("row %+v", e)
			}
		}
	})

	t.Run("lane blocked", func(t *testing.T) {
		s := newFakeServer()
		blocking := inboxRow(7, EventPending)
		blocking.replays = 1
		early := s.addEvent(blocking)
		late := inboxRow(7, EventPublished)
		late.deferrals = MaxDeferrals
		id := s.addEvent(late)
		start := time.Now()
		outcome, err := takeEvent(t, s, 7, id, finish)
		if err != nil || outcome != TakeParked {
			t.Fatalf("%v, %v", outcome, err)
		}
		ev := s.events()
		if ev[0].id != early || ev[0].state != EventDead || ev[0].err != "lane_blocked" {
			t.Errorf("blocking row %+v", ev[0])
		}
		if due := time.UnixMilli(ev[0].dueAt).Sub(start); due < ReplayDelay(1)-time.Second || due > ReplayDelay(1)+time.Minute {
			t.Errorf("replayed in %v, want %v", due, ReplayDelay(1))
		}
		if ev[1].state != EventParked {
			t.Errorf("row %+v not parked", ev[1])
		}
	})

	t.Run("failed", func(t *testing.T) {
		s := newFakeServer()
		id := s.addEvent(inboxRow(7, EventPublished))
		cause := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'secret' for key"}
		outcome, err := takeEvent(t, s, 7, id, func(b *Box) error { return b.FailEvent(cause, 3) })
		if err != nil || outcome != TakeApply {
			t.Fatalf("%v, %v", outcome, err)
		}
		e := s.events()[0]
		if e.state != EventDead || e.attempts != 3 || e.err != "mysql 1062" {
			t.Errorf("row %+v", e)
		}
	})

	t.Run("two events", func(t *testing.T) {
		s := newFakeServer()
		a, b := s.addEvent(inboxRow(7, EventPublished)), s.addEvent(inboxRow(7, EventPublished))
		_, err := Run(context.Background(), s.open(t), 7, func(box *Box) error {
			if _, err := box.TakeEvent(a); err != nil {
				return err
			}
			_, err := box.TakeEvent(b)
			return err
		})
		if !errors.Is(err, ErrMisuse) {
			t.Fatalf("error %v", err)
		}
	})
}

func TestReplayDelay(t *testing.T) {
	for replays, want := range []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 4 * time.Hour} {
		if got := ReplayDelay(replays); got != want {
			t.Errorf("ReplayDelay(%d) = %v, want %v", replays, got, want)
		}
	}
	if got := ReplayDelay(1000); got != MaxDeadDelay {
		t.Errorf("ReplayDelay(1000) = %v", got)
	}
}

func TestDeterministic(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{mtproto.ErrMessageIdInvalid, true},
		{fmt.Errorf("wrapped: %w", mtproto.ErrPeerIdInvalid), true},
		{mtproto.NewRpcError(mtproto.ErrUserIdInvalid), true},
		{&mysql.MySQLError{Number: 1062}, true},
		{&mysql.MySQLError{Number: 3140}, true},
		{errStateChanged, true},
		{ErrEventNotFinished, true},
		{ErrInsideRun, true},
		{fmt.Errorf("%w: bad", ErrInvalidEvent), true},
		{misuse("pts_count"), true},
		// Temporary: everything else.
		{&mysql.MySQLError{Number: 1213}, false},
		{&mysql.MySQLError{Number: 1053}, false},
		{&mysql.MySQLError{Number: 1040}, false},
		{&mysql.MySQLError{Number: 2013}, false},
		{ErrCommitUnknown, false},
		{ErrBudget, false},
		{errEventGone, false},
		{mysql.ErrInvalidConn, false},
		{status.Error(codes.Unavailable, "down"), false},
		{status.Error(codes.DeadlineExceeded, "slow"), false},
		{context.DeadlineExceeded, false},
		{errors.New("something new"), false},
		{nil, false},
	} {
		if got := Deterministic(c.err); got != c.want {
			t.Errorf("Deterministic(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestEventError(t *testing.T) {
	if got := EventError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'hello' for key 'x'"}); got != "mysql 1062" {
		t.Errorf("mysql error: %q", got)
	}
	if got := EventError(mtproto.ErrMessageIdInvalid); got != "rpc 400 MESSAGE_ID_INVALID" {
		t.Errorf("rpc error: %q", got)
	}
	if got := EventError(errors.New(string(bytes.Repeat([]byte("x"), 1000)))); len(got) != errorBytes {
		t.Errorf("long error kept %d bytes", len(got))
	}
}
