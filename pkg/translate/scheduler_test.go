// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package translate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

// schedEngine answers the plain prompts of the hy-mt1.5 profile with the
// upper case of their text. A text in hold waits in the engine until its
// channel is closed; answer, when set, may fail a call.
type schedEngine struct {
	mu      sync.Mutex
	calls   []string
	hold    map[string]chan struct{}
	answer  func(src string, n int) (string, error)
	healthy atomic.Bool
	started chan string
}

func newSchedEngine() *schedEngine {
	e := &schedEngine{hold: make(map[string]chan struct{}), started: make(chan string, 100)}
	e.healthy.Store(true)
	return e
}

func (e *schedEngine) holdText(src string) func() {
	ch := make(chan struct{})
	e.mu.Lock()
	e.hold[src] = ch
	e.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func (e *schedEngine) Translate(ctx context.Context, req Request) (string, error) {
	src := req.Prompt[strings.Index(req.Prompt, "\n\n")+2:]
	e.mu.Lock()
	e.calls = append(e.calls, src)
	n := 0
	for _, c := range e.calls {
		if c == src {
			n++
		}
	}
	ch := e.hold[src]
	answer := e.answer
	e.mu.Unlock()
	e.started <- src
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return "", fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
		}
	}
	if answer != nil {
		return answer(src, n)
	}
	return strings.ToUpper(src), nil
}

func (e *schedEngine) Health(context.Context) error {
	if e.healthy.Load() {
		return nil
	}
	return ErrDown
}

func (e *schedEngine) callsOf(src string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, c := range e.calls {
		if c == src {
			n++
		}
	}
	return n
}

func (e *schedEngine) order() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

func (e *schedEngine) waitStarted(t *testing.T, src string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case s := <-e.started:
			if s == src {
				return
			}
		case <-timeout:
			t.Fatalf("%q never reached the engine", src)
		}
	}
}

// memCache is the cache in memory; with drop it forgets what it is given.
type memCache struct {
	mu    sync.Mutex
	rows  map[Key]*CachedTranslation
	reads int
	drop  bool
}

func newMemCache() *memCache { return &memCache{rows: make(map[Key]*CachedTranslation)} }

func (c *memCache) CachedTranslations(_ context.Context, keys []Key) (map[Key]*CachedTranslation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	found := make(map[Key]*CachedTranslation)
	for _, k := range keys {
		if t, ok := c.rows[k]; ok {
			found[k] = t
		}
	}
	return found, nil
}

func (c *memCache) PutTranslation(_ context.Context, key Key, _, _ string, t *CachedTranslation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.drop {
		c.rows[key] = t
	}
	return nil
}

func (c *memCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rows)
}

func (c *memCache) readCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// countLimits counts the items reserved; refuse refuses them all.
type countLimits struct {
	mu       sync.Mutex
	reserved int
	refuse   bool
}

func (l *countLimits) Reserve(_ context.Context, _ int64, n int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.refuse {
		return FloodWait(20 * time.Second)
	}
	l.reserved += n
	return nil
}

type schedFixture struct {
	s      *Scheduler
	engine *schedEngine
	cache  *memCache
	limits *countLimits
	clock  atomic.Int64 // added to the real time, ns
}

func newSchedFixture(t *testing.T, edit func(*Config)) *schedFixture {
	t.Helper()
	c := Config{URL: "http://translate:8080", Model: "hy-mt", Slots: "1"}
	if edit != nil {
		edit(&c)
	}
	c = c.WithDefaults()
	f := &schedFixture{engine: newSchedEngine(), cache: newMemCache(), limits: &countLimits{}}
	s, err := newScheduler(c, testProfile(t), f.engine, f.cache, f.limits)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(time.Duration(f.clock.Load())) }
	s.retryPauses = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	s.healthEvery = 20 * time.Millisecond
	s.start()
	t.Cleanup(s.Stop)
	f.s = s
	return f
}

func plain(texts ...string) []*mtproto.TextWithEntities {
	out := make([]*mtproto.TextWithEntities, len(texts))
	for i, s := range texts {
		out[i] = mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: s}).To_TextWithEntities()
	}
	return out
}

type answer struct {
	texts []string
	err   error
}

func (f *schedFixture) ask(user int64, prio Priority, texts ...string) answer {
	res, err := f.s.Translate(context.Background(), Ask{Method: "messages.translateText", UserId: user, Lang: "en", Priority: prio, Items: plain(texts...)})
	a := answer{err: err}
	for _, r := range res {
		a.texts = append(a.texts, r.GetText())
	}
	return a
}

func (f *schedFixture) goAsk(user int64, prio Priority, texts ...string) <-chan answer {
	ch := make(chan answer, 1)
	go func() { ch <- f.ask(user, prio, texts...) }()
	return ch
}

func (f *schedFixture) queued(prio Priority) int {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.lanes[prio].n
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func receive(t *testing.T, ch <-chan answer) answer {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
		return answer{}
	}
}

// floodSeconds returns the X of FLOOD_WAIT_X, or -1 for another error.
func floodSeconds(err error) int {
	var x int
	if _, scanErr := fmt.Sscanf(status.Convert(err).Message(), "FLOOD_WAIT_%d", &x); scanErr != nil || err == nil {
		return -1
	}
	return x
}

func wantTexts(t *testing.T, a answer, want ...string) {
	t.Helper()
	if a.err != nil {
		t.Fatalf("answer: %v, want %q", a.err, want)
	}
	if !slices.Equal(a.texts, want) {
		t.Fatalf("answer %q, want %q", a.texts, want)
	}
}

func TestSchedulerPrioritiesAndTurns(t *testing.T) {
	f := newSchedFixture(t, nil)
	release := f.engine.holdText("blocker")
	blocker := f.goAsk(9, PriorityChat, "blocker")
	f.engine.waitStarted(t, "blocker")

	a := f.goAsk(1, PriorityChat, "a one", "a two", "a three")
	eventually(t, "the batch of A", func() bool { return f.queued(PriorityChat) == 3 })
	b := f.goAsk(2, PriorityChat, "b one", "shared")
	eventually(t, "the batch of B", func() bool { return f.queued(PriorityChat) == 5 })
	c := f.goAsk(3, PriorityManual, "c one")
	eventually(t, "the text of C", func() bool { return f.queued(PriorityManual) == 1 })
	// D waits for an item of B's batch: it moves up to D's priority.
	d := f.goAsk(4, PriorityManual, "shared")
	eventually(t, "the promotion", func() bool { return f.queued(PriorityManual) == 2 })
	release()

	wantTexts(t, receive(t, blocker), "BLOCKER")
	wantTexts(t, receive(t, a), "A ONE", "A TWO", "A THREE")
	wantTexts(t, receive(t, b), "B ONE", "SHARED")
	wantTexts(t, receive(t, c), "C ONE")
	wantTexts(t, receive(t, d), "SHARED")
	want := []string{"blocker", "c one", "shared", "a one", "b one", "a two", "a three"}
	if got := f.engine.order(); !slices.Equal(got, want) {
		t.Fatalf("the engine got %q, want %q", got, want)
	}
}

func TestSchedulerSingleflight(t *testing.T) {
	f := newSchedFixture(t, func(c *Config) { c.Slots = "2" })
	// What is given to the cache is lost: only the shared job can keep
	// the engine to one call.
	f.cache.drop = true
	release := f.engine.holdText("same text")
	first := f.goAsk(1, PriorityChat, "same text")
	f.engine.waitStarted(t, "same text")
	second := f.goAsk(2, PriorityManual, "same text", "same text")
	eventually(t, "the second request", func() bool { return f.cache.readCount() == 2 })
	time.Sleep(50 * time.Millisecond)
	release()
	wantTexts(t, receive(t, first), "SAME TEXT")
	wantTexts(t, receive(t, second), "SAME TEXT", "SAME TEXT")
	if n := f.engine.callsOf("same text"); n != 1 {
		t.Fatalf("the engine translated it %d times, want 1", n)
	}
	if f.limits.reserved != 1 {
		t.Fatalf("reserved %d items, want 1", f.limits.reserved)
	}
}

func TestSchedulerSyncWaitThenCache(t *testing.T) {
	f := newSchedFixture(t, func(c *Config) { c.SyncWait = 50 * time.Millisecond })
	release := f.engine.holdText("slow one")
	a := f.ask(1, PriorityChat, "slow one", "quick one")
	if x := floodSeconds(a.err); x < 3 || x > 30 {
		t.Fatalf("answer %v, want FLOOD_WAIT_X with X from 3 to 30", a.err)
	}
	// The work goes on after the answer and reaches the cache.
	release()
	eventually(t, "the cache", func() bool { return f.cache.size() == 2 })
	wantTexts(t, f.ask(1, PriorityChat, "slow one", "quick one"), "SLOW ONE", "QUICK ONE")
	if n := len(f.engine.order()); n != 2 {
		t.Fatalf("the engine got %d calls, want 2", n)
	}
}

func TestSchedulerRetries(t *testing.T) {
	f := newSchedFixture(t, nil)
	f.engine.answer = func(src string, n int) (string, error) {
		switch {
		case src == "flaky" && n <= 2:
			return "", fmt.Errorf("%w: cut by max_tokens", ErrBadAnswer)
		case src == "broken":
			return "", fmt.Errorf("%w: status 500", ErrUnavailable)
		case src == "refused":
			return "", fmt.Errorf("%w: status 400", ErrRejected)
		}
		return strings.ToUpper(src), nil
	}
	wantTexts(t, f.ask(1, PriorityChat, "flaky"), "FLAKY")
	if n := f.engine.callsOf("flaky"); n != 3 {
		t.Fatalf("flaky: %d calls, want 3", n)
	}

	// A 500 makes the engine wait for its health check: the request is
	// answered at once, and the item is tried twice more, then refused
	// for FailedTTL.
	if a := f.ask(1, PriorityChat, "broken"); floodSeconds(a.err) != 10 {
		t.Fatalf("broken: %v, want FLOOD_WAIT_10", a.err)
	}
	f.waitFailed(t, "broken", 3)
	a := f.ask(1, PriorityChat, "broken", "other")
	if x := floodSeconds(a.err); x != 30 {
		t.Fatalf("broken again: %v, want FLOOD_WAIT_30 (60 s of FailedTTL left, at most 30)", a.err)
	}
	if n := f.engine.callsOf("broken"); n != 3 {
		t.Fatalf("broken again: %d calls, want still 3", n)
	}
	// The other item of that request is translated meanwhile.
	eventually(t, "the other item", func() bool { return f.cache.size() == 2 })
	f.clock.Add(int64(DefaultFailedTTL + time.Second))
	if a := f.ask(1, PriorityChat, "broken"); floodSeconds(a.err) != 10 {
		t.Fatalf("broken after FailedTTL: %v, want FLOOD_WAIT_10", a.err)
	}
	f.waitFailed(t, "broken", 6)

	// A rejected item is not tried again.
	if a := f.ask(1, PriorityChat, "refused"); floodSeconds(a.err) < 3 {
		t.Fatalf("refused: %v, want FLOOD_WAIT_X", a.err)
	}
	if n := f.engine.callsOf("refused"); n != 1 {
		t.Fatalf("refused: %d calls, want 1", n)
	}
}

// waitFailed waits until src failed after calls calls in all and the
// engine takes items again.
func (f *schedFixture) waitFailed(t *testing.T, src string, calls int) {
	t.Helper()
	eventually(t, src+" to fail", func() bool {
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		return f.engine.callsOf(src) == calls && len(f.s.jobs) == 0 && f.s.healthy
	})
}

func TestSchedulerEngineDown(t *testing.T) {
	f := newSchedFixture(t, nil)
	var down atomic.Bool
	down.Store(true)
	f.engine.healthy.Store(false)
	f.engine.answer = func(src string, _ int) (string, error) {
		if down.Load() {
			return "", fmt.Errorf("%w (status 503)", ErrBusy)
		}
		return strings.ToUpper(src), nil
	}
	start := time.Now()
	if a := f.ask(1, PriorityChat, "first"); floodSeconds(a.err) != 10 {
		t.Fatalf("first: %v, want FLOOD_WAIT_10", a.err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the engine going down was answered after %v, not at once", took)
	}
	// No new item is queued while the engine is waited for.
	if a := f.ask(2, PriorityManual, "second"); floodSeconds(a.err) != 10 {
		t.Fatalf("second: %v, want FLOOD_WAIT_10", a.err)
	}
	f.s.mu.Lock()
	jobs := len(f.s.jobs)
	f.s.mu.Unlock()
	if jobs != 1 {
		t.Fatalf("%d items wait, want only the first", jobs)
	}
	time.Sleep(100 * time.Millisecond)
	if n := f.engine.callsOf("first"); n != 1 {
		t.Fatalf("first was sent %d times while the engine was down, want 1", n)
	}

	// The health check passes: the first item goes on into the cache.
	down.Store(false)
	f.engine.healthy.Store(true)
	eventually(t, "the cache", func() bool { return f.cache.size() == 1 })
	wantTexts(t, f.ask(1, PriorityChat, "first"), "FIRST")
	wantTexts(t, f.ask(2, PriorityManual, "second"), "SECOND")
	if n := f.engine.callsOf("second"); n != 1 {
		t.Fatalf("second: %d calls, want 1", n)
	}
}

func TestSchedulerStop(t *testing.T) {
	f := newSchedFixture(t, func(c *Config) { c.SyncWait = 5 * time.Second })
	release := f.engine.holdText("running")
	running := f.goAsk(1, PriorityChat, "running")
	f.engine.waitStarted(t, "running")
	queued := f.goAsk(2, PriorityChat, "queued")
	eventually(t, "the queue", func() bool { return f.queued(PriorityChat) == 1 })

	stopped := make(chan struct{})
	go func() {
		f.s.Stop()
		close(stopped)
	}()
	if a := receive(t, queued); floodSeconds(a.err) != 10 {
		t.Fatalf("queued: %v, want FLOOD_WAIT_10", a.err)
	}
	select {
	case <-stopped:
		t.Fatal("Stop did not wait for the item in the engine")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	<-stopped
	wantTexts(t, receive(t, running), "RUNNING")
	if f.cache.size() != 1 {
		t.Fatal("the translation that came back during Stop is not cached")
	}
	if a := f.ask(1, PriorityChat, "after"); floodSeconds(a.err) != 10 {
		t.Fatalf("after Stop: %v, want FLOOD_WAIT_10", a.err)
	}
	if n := f.engine.callsOf("queued"); n != 0 {
		t.Fatalf("queued was sent %d times", n)
	}
}

func TestSchedulerStopGrace(t *testing.T) {
	f := newSchedFixture(t, nil)
	f.s.stopGrace = 50 * time.Millisecond
	f.engine.holdText("stuck")
	stuck := f.goAsk(1, PriorityChat, "stuck")
	f.engine.waitStarted(t, "stuck")
	start := time.Now()
	f.s.Stop()
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Stop took %v past its grace", took)
	}
	if a := receive(t, stuck); floodSeconds(a.err) != 10 {
		t.Fatalf("stuck: %v, want FLOOD_WAIT_10", a.err)
	}
}

func TestSchedulerLimitsCountEngineItemsOnly(t *testing.T) {
	f := newSchedFixture(t, nil)
	wantTexts(t, f.ask(1, PriorityChat, "one", "two"), "ONE", "TWO")
	if f.limits.reserved != 2 {
		t.Fatalf("reserved %d, want 2", f.limits.reserved)
	}
	// Cached, empty and verbatim items are free.
	code := mtproto.MakeTLMessageEntityCode(&mtproto.MessageEntity{Offset: 0, Length: 5}).To_MessageEntity()
	items := append(plain("one", "", "  "), mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{
		Text: "ls -l", Entities: []*mtproto.MessageEntity{code},
	}).To_TextWithEntities(), mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "👍 !"}).To_TextWithEntities())
	res, err := f.s.Translate(context.Background(), Ask{UserId: 1, Lang: "en", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(res))
	for i, r := range res {
		got[i] = r.GetText()
	}
	if want := []string{"ONE", "", "", "ls -l", "👍 !"}; !slices.Equal(got, want) {
		t.Fatalf("answer %q, want %q", got, want)
	}
	if len(res[3].GetEntities()) != 1 {
		t.Fatalf("the verbatim item lost its entities: %v", res[3].GetEntities())
	}
	if f.limits.reserved != 2 || len(f.engine.order()) != 2 {
		t.Fatalf("reserved %d and sent %d, want 2 and 2", f.limits.reserved, len(f.engine.order()))
	}

	f.limits.refuse = true
	if a := f.ask(1, PriorityChat, "three"); floodSeconds(a.err) != 20 {
		t.Fatalf("over the limit: %v, want the limit's FLOOD_WAIT_20", a.err)
	}
	f.s.mu.Lock()
	jobs := len(f.s.jobs)
	f.s.mu.Unlock()
	if jobs != 0 || f.engine.callsOf("three") != 0 {
		t.Fatal("an item over the limit was queued")
	}
}

func TestSchedulerRequestChecks(t *testing.T) {
	f := newSchedFixture(t, nil)
	for _, tc := range []struct {
		name string
		ask  Ask
		want error
	}{
		{"no items", Ask{Lang: "en"}, mtproto.ErrInputTextEmpty},
		{"too long", Ask{Lang: "en", Items: plain(strings.Repeat("a", DefaultMaxItemLength+1))}, ErrInputTextTooLong},
		{"bad lang", Ask{Lang: "xx", Items: plain("hello")}, ErrToLangInvalid},
	} {
		if _, err := f.s.Translate(context.Background(), tc.ask); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestSchedulerCanceledCaller(t *testing.T) {
	f := newSchedFixture(t, nil)
	release := f.engine.holdText("long")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.s.Translate(ctx, Ask{UserId: 1, Lang: "en", Items: plain("long")})
		done <- err
	}()
	f.engine.waitStarted(t, "long")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller: %v", err)
	}
	release()
	eventually(t, "the cache", func() bool { return f.cache.size() == 1 })
}

// The cache keeps what became of the formatting: an item whose answer lost
// the tags but that had only verbatim entities kept everything, one with
// bold lost it (translation_cache.markup).
func TestSchedulerCachesMarkup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		entity *mtproto.MessageEntity
		want   Markup
	}{
		{"verbatim only", mtproto.MakeTLMessageEntityMention(&mtproto.MessageEntity{Offset: 6, Length: 4}).To_MessageEntity(), MarkupKept},
		{"formatting", mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 6, Length: 4}).To_MessageEntity(), MarkupLost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSchedFixture(t, nil)
			// The engine answers a tagged prompt without <target>: the
			// item goes by pieces.
			item := mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{
				Text:     "hello @bob and the others",
				Entities: []*mtproto.MessageEntity{tc.entity},
			}).To_TextWithEntities()
			if _, err := f.s.Translate(context.Background(), Ask{UserId: 1, Lang: "en", Items: []*mtproto.TextWithEntities{item}}); err != nil {
				t.Fatal(err)
			}
			f.cache.mu.Lock()
			defer f.cache.mu.Unlock()
			if len(f.cache.rows) != 1 {
				t.Fatalf("%d cached rows, want 1", len(f.cache.rows))
			}
			for _, row := range f.cache.rows {
				if row.Markup != tc.want {
					t.Fatalf("cached markup %d, want %d", row.Markup, tc.want)
				}
			}
		})
	}
}
