package sender

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testPhone = "79991234567"
	testCode  = "654321"
)

type fakeMessenger struct {
	mu         sync.Mutex
	session    string
	deliveries []string
	removed    [][]int64
	result     Delivery
	block      chan struct{}
	removeErr  atomic.Bool
	at         []time.Time
}

func newFake() *fakeMessenger {
	return &fakeMessenger{session: SessionReady, result: Delivery{Result: Sent, UserID: 5}}
}

func (f *fakeMessenger) Session() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.session
}

func (f *fakeMessenger) Deliver(_ context.Context, phone, text string) Delivery {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveries = append(f.deliveries, phone+"|"+text)
	f.at = append(f.at, time.Now())
	return f.result
}

func (f *fakeMessenger) RemoveContacts(_ context.Context, ids []int64) error {
	if f.removeErr.Load() {
		return errors.New("flaky")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, ids)
	return nil
}

func testLimits() Limits {
	l := DefaultLimits
	l.MinGap = time.Millisecond
	l.MaxJitter = 0
	l.RemovalRetryPeriod = 10 * time.Millisecond
	return l
}

func startQueue(t *testing.T, f Messenger, l Limits) (*Queue, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	return runQueue(t, f, l, path), path
}

func runQueue(t *testing.T, f Messenger, l Limits, path string) *Queue {
	t.Helper()
	q, err := Open(f, l, path)
	if err != nil {
		t.Fatal(err)
	}
	stopWhenDone(t, q)
	return q
}

// stopWhenDone runs q until the test ends, and waits for it: the queue
// writes its state file, which the test's directory must not outlive.
func stopWhenDone(t *testing.T, q *Queue) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		q.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
}

func submit(q *Queue, phone string) Result {
	return q.Submit(context.Background(), phone, testCode, 5*time.Second)
}

func TestDeliversAndRemovesTheContact(t *testing.T) {
	f := newFake()
	q, _ := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != Sent {
		t.Fatalf("result %s", r)
	}
	if len(f.deliveries) != 1 || !strings.HasPrefix(f.deliveries[0], testPhone+"|") || !strings.Contains(f.deliveries[0], testCode) {
		t.Fatalf("deliveries %q", f.deliveries)
	}
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.removed) == 1 && f.removed[0][0] == 5
	})
}

func TestNoAccountNoRemoval(t *testing.T) {
	f := newFake()
	f.result = Delivery{Result: NoTelegram}
	q, _ := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != NoTelegram {
		t.Fatalf("result %s", r)
	}
	time.Sleep(20 * time.Millisecond)
	if len(f.removed) != 0 {
		t.Fatalf("removed %v", f.removed)
	}
}

func TestStateFileHoldsNoPhoneOrCode(t *testing.T) {
	f := newFake()
	q, path := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != Sent {
		t.Fatalf("result %s", r)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testPhone) || strings.Contains(string(data), testCode) {
		t.Fatalf("the state file leaks: %s", data)
	}
}

func TestPerRecipientLimitSurvivesRestart(t *testing.T) {
	l := testLimits()
	l.PerRecipientHour = 2
	f := newFake()
	q, path := startQueue(t, f, l)
	for range 2 {
		if r := submit(q, testPhone); r != Sent {
			t.Fatalf("result %s", r)
		}
	}
	if r := submit(q, testPhone); r != RecipientLimited {
		t.Fatalf("result %s, want the recipient limit", r)
	}
	if r := submit(q, "79990000000"); r != Sent {
		t.Fatalf("another phone: %s", r)
	}

	reopened := runQueue(t, f, l, path)
	if r := submit(reopened, testPhone); r != RecipientLimited {
		t.Fatalf("after a restart: %s, want the recipient limit", r)
	}
}

func TestBudgetOfTheAccount(t *testing.T) {
	l := testLimits()
	l.PerHour = 1
	f := newFake()
	q, _ := startQueue(t, f, l)
	if r := submit(q, testPhone); r != Sent {
		t.Fatalf("result %s", r)
	}
	if r := submit(q, "79990000000"); r != AccountLimited {
		t.Fatalf("result %s, want the account budget", r)
	}
}

func TestPauseBetweenSends(t *testing.T) {
	l := testLimits()
	l.MinGap = 60 * time.Millisecond
	f := newFake()
	q, _ := startQueue(t, f, l)
	for i := range 3 {
		if r := submit(q, "7999123456"+string(rune('0'+i))); r != Sent {
			t.Fatalf("result %s", r)
		}
	}
	for i := 1; i < len(f.at); i++ {
		if gap := f.at[i].Sub(f.at[i-1]); gap < l.MinGap-2*time.Millisecond {
			t.Fatalf("sends %s apart, want at least %s", gap, l.MinGap)
		}
	}
}

func TestFloodWaitBlocksAndSurvivesRestart(t *testing.T) {
	f := newFake()
	f.result = Delivery{Result: AccountLimited, UserID: 0, RetryAfter: time.Hour}
	q, path := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != AccountLimited {
		t.Fatalf("result %s", r)
	}
	if r := submit(q, "79990000000"); r != AccountLimited {
		t.Fatalf("during the wait: %s", r)
	}
	if st := q.State(); st.LimitedUntil == 0 || st.Sendable {
		t.Fatalf("state %+v", st)
	}
	f.result = Delivery{Result: Sent, UserID: 5}
	reopened := runQueue(t, f, testLimits(), path)
	if r := submit(reopened, "79990000001"); r != AccountLimited {
		t.Fatalf("after a restart: %s", r)
	}
}

func TestPeerFloodCoolsDown(t *testing.T) {
	f := newFake()
	f.result = Delivery{Result: AccountLimited, Banned: true}
	q, _ := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != AccountLimited {
		t.Fatalf("result %s", r)
	}
	want := time.Now().Add(DefaultLimits.BannedCooldown - time.Minute).Unix()
	if st := q.State(); st.LimitedUntil < want {
		t.Fatalf("limited until %d, want after %d", st.LimitedUntil, want)
	}
}

func TestNoSessionIsUnavailable(t *testing.T) {
	f := newFake()
	f.session = SessionNoSession
	q, _ := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != Unavailable {
		t.Fatalf("result %s", r)
	}
	if st := q.State(); st.Session != SessionNoSession || st.Sendable {
		t.Fatalf("state %+v", st)
	}
}

func TestFullQueueIsBusy(t *testing.T) {
	l := testLimits()
	l.QueueLength = 1
	f := newFake()
	f.block = make(chan struct{})
	defer close(f.block)
	q, _ := startQueue(t, f, l)

	// One held by the account, one waiting in the queue, the third refused.
	go q.Submit(context.Background(), testPhone, testCode, time.Minute)
	time.Sleep(20 * time.Millisecond)
	go q.Submit(context.Background(), "79990000000", testCode, time.Minute)
	time.Sleep(20 * time.Millisecond)
	if r := q.Submit(context.Background(), "79990000001", testCode, time.Second); r != Busy {
		t.Fatalf("result %s", r)
	}
}

func TestReturnsQueuedAndSendsLater(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	q, _ := startQueue(t, f, testLimits())
	if r := q.Submit(context.Background(), testPhone, testCode, 20*time.Millisecond); r != Queued {
		t.Fatalf("result %s", r)
	}
	close(f.block)
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.deliveries) == 1
	})
}

func TestExpiredInTheQueue(t *testing.T) {
	l := testLimits()
	l.MaxWaitInQueue = time.Hour
	f := newFake()
	path := filepath.Join(t.TempDir(), "state.json")
	q, err := Open(f, l, path)
	if err != nil {
		t.Fatal(err)
	}
	var now atomic.Int64
	now.Store(1_800_000_000)
	q.now = func() time.Time { return time.Unix(now.Load(), 0) }

	done := make(chan Result, 1)
	go func() { done <- submit(q, testPhone) }()
	waitFor(t, func() bool { return len(q.items) == 1 })
	now.Add(int64(2 * time.Hour / time.Second))
	stopWhenDone(t, q)

	if r := <-done; r != Expired {
		t.Fatalf("result %s", r)
	}
	if len(f.deliveries) != 0 {
		t.Fatalf("expired code was sent: %q", f.deliveries)
	}
}

func TestContactRemovalIsRetried(t *testing.T) {
	f := newFake()
	f.removeErr.Store(true)
	q, path := startQueue(t, f, testLimits())
	if r := submit(q, testPhone); r != Sent {
		t.Fatalf("result %s", r)
	}
	waitFor(t, func() bool { return q.State().ContactsToRemove == 1 })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"to_remove":[5]`) {
		t.Fatalf("the failed removal is not in the state file: %s", data)
	}
	f.removeErr.Store(false)
	waitFor(t, func() bool { return q.State().ContactsToRemove == 0 })
}

// hangingRemoval never answers a removal, as TDLib without a network.
type hangingRemoval struct{ *fakeMessenger }

func (hangingRemoval) RemoveContacts(ctx context.Context, _ []int64) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestHangingRemovalDoesNotStopTheQueue(t *testing.T) {
	l := testLimits()
	l.DeliverTimeout = 20 * time.Millisecond
	q, _ := startQueue(t, hangingRemoval{newFake()}, l)
	if r := submit(q, testPhone); r != Sent {
		t.Fatalf("result %s", r)
	}
	waitFor(t, func() bool { return q.State().ContactsToRemove == 1 })
	if r := submit(q, "79991234568"); r != Sent {
		t.Fatalf("the next code: %s", r)
	}
}

func TestOldRecipientsLeaveTheState(t *testing.T) {
	q, err := Open(newFake(), testLimits(), filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_000_000, 0)
	q.now = func() time.Time { return now }
	if r := q.reserve(testPhone); r != "" {
		t.Fatalf("first: %s", r)
	}
	now = now.Add(25 * time.Hour)
	if r := q.reserve("79991234568"); r != "" {
		t.Fatalf("second: %s", r)
	}
	if _, kept := q.st.Recipients[q.recipientKey(testPhone)]; kept || len(q.st.Recipients) != 1 {
		t.Fatalf("recipients: %v", q.st.Recipients)
	}
}

func TestDamagedStateFileStopsTheStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(newFake(), testLimits(), path); err == nil {
		t.Fatal("a damaged state file must not start a fresh limit")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
