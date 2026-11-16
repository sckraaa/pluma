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
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// The scheduler gives the items that are not in the cache to the engine
// (ADR 0064, section 6). The queue is in memory: a translation is the
// answer to a screen that is open, not a promised update, and a restart
// loses only what the clients ask for again. Its rules:
//   - at most Slots items are in the engine at once, one request each (the
//     fallback pieces of an item go one after another in its slot);
//   - one text a person waits for (PriorityManual) goes before the batches
//     of chats (PriorityChat); within a priority the users take turns, so
//     that the chats of one do not hold up the others;
//   - an item asked again while it is queued or in work is not queued
//     again: every request waits for the same work (singleflight by the
//     key of the cache), and a person who waits for an item of a batch
//     moves it up to his priority;
//   - a request waits up to SyncWait (SyncWaitSingle for PriorityManual);
//     what is not ready by then is answered with FLOOD_WAIT_X, and the work
//     goes on into the cache, where the repeat of the client finds it;
//   - a failed attempt is tried again twice, after 1 and 3 seconds; then
//     the item is answered with FLOOD_WAIT_X for FailedTTL;
//   - after the engine was unavailable no new item is queued, and the
//     answer is FLOOD_WAIT_10, until its health check passes (every 5 s)
//     or an item in work comes back translated. Waiting for an engine that
//     is down or busy is not an attempt;
//   - Stop answers the queued items and waits up to 10 s for those in
//     the engine; their translations still go to the cache.

// Priority orders the items of the queue.
type Priority int8

const (
	// PriorityChat is a batch of the messages of a chat: the translation
	// bar, a channel with automatic translation.
	PriorityChat Priority = iota
	// PriorityManual is one text a person asked for and waits for: the
	// translation of one message, the translation screen of iOS.
	PriorityManual

	priorities = 2
)

// Ask is one request to translate.
type Ask struct {
	// Method is the TL method, the label of translate_requests_total.
	Method string
	UserId int64
	// Lang is a language of the profile (Profile.Lang), not the client's
	// spelling of it.
	Lang string
	// Priority is PriorityManual or PriorityChat; anything else is
	// PriorityChat.
	Priority Priority
	// Items are the texts to translate. Their input mentions are resolved
	// beforehand (Normalize with a UserExistence); what is left of them
	// is dropped, as Translate drops it. An empty text is an empty item.
	Items []*mtproto.TextWithEntities
}

// Cache is where the scheduler reads and writes translations: *Store.
type Cache interface {
	CachedTranslations(ctx context.Context, keys []Key) (map[Key]*CachedTranslation, error)
	PutTranslation(ctx context.Context, key Key, toLang, model string, t *CachedTranslation) error
}

// cacheCounter is a Cache that can count its rows (translate_cache_rows).
type cacheCounter interface {
	CacheRows(ctx context.Context) (int64, error)
}

const (
	// engineDownWait is the X of FLOOD_WAIT_X while the engine is
	// waited for (brief 101, "Пределы и ошибки").
	engineDownWait = 10 * time.Second
	// healthEvery is how often an unavailable engine is checked.
	healthEvery = 5 * time.Second
	// stopGrace is how long Stop waits for the items in the engine.
	stopGrace = 10 * time.Second
	// putTimeout bounds the write of one translation to the cache; it
	// outlives a Stop, so that a finished translation is not lost.
	putTimeout = 5 * time.Second
	// failedSweepEvery is how often the expired failures are forgotten.
	failedSweepEvery = time.Minute
	// cacheRowsEvery is how often translate_cache_rows is counted.
	cacheRowsEvery = 5 * time.Minute
	// initialItemTime is what an item is thought to take in the engine
	// before one has been measured; only the X of FLOOD_WAIT_X depends on
	// it.
	initialItemTime = 3 * time.Second
)

// defaultRetryPauses are the pauses before the second and the third
// attempt of an item.
var defaultRetryPauses = []time.Duration{time.Second, 3 * time.Second}

var (
	// errNeedsEngine is what the probe of an item answers when the item
	// needs the engine (passThrough).
	errNeedsEngine = errors.New("translate: the item needs the engine")
	// errStopped ends the items the scheduler gives up at Stop.
	errStopped = errors.New("translate: the scheduler is stopped")
)

// job is one item in the queue, in the engine, or waiting to be tried
// again. Every request that needs its key waits for it.
type job struct {
	key   Key
	lang  string
	src   *mtproto.TextWithEntities
	chars int
	// owner and prio place the job in the queue: the user whose turn it
	// takes and the lane.
	owner int64
	prio  Priority
	state jobState
	// attempt counts the attempts that came to nothing.
	attempt int
	retry   *time.Timer

	// Set once, before done is closed.
	done        chan struct{}
	finished    bool
	res         *CachedTranslation
	err         error
	failedUntil time.Time
}

type jobState int8

const (
	jobQueued jobState = iota
	jobRunning
	jobDelayed // waiting for its retry pause
)

// lane is the queue of one priority: a FIFO per user, and the users with
// items in the order of their turns.
type lane struct {
	users map[int64][]*job
	turn  []int64
	n     int
}

func (l *lane) push(user int64, j *job, front bool) {
	if l.users == nil {
		l.users = make(map[int64][]*job)
	}
	q, had := l.users[user]
	if front {
		q = append([]*job{j}, q...)
	} else {
		q = append(q, j)
	}
	l.users[user] = q
	if !had {
		l.turn = append(l.turn, user)
	}
	l.n++
}

// pop takes the first item of the user whose turn it is; the user then
// waits behind the others.
func (l *lane) pop() *job {
	if len(l.turn) == 0 {
		return nil
	}
	user := l.turn[0]
	q := l.users[user]
	j := q[0]
	q[0] = nil
	q = q[1:]
	l.turn = l.turn[1:]
	if len(q) == 0 {
		delete(l.users, user)
	} else {
		l.users[user] = q
		l.turn = append(l.turn, user)
	}
	l.n--
	return j
}

// remove takes j out of the queue of user; it reports whether j was there.
func (l *lane) remove(user int64, j *job) bool {
	q := l.users[user]
	i := slices.Index(q, j)
	if i < 0 {
		return false
	}
	q = slices.Delete(q, i, i+1)
	if len(q) == 0 {
		delete(l.users, user)
		l.turn = slices.DeleteFunc(l.turn, func(u int64) bool { return u == user })
	} else {
		l.users[user] = q
	}
	l.n--
	return true
}

// Scheduler queues the items of the requests for the engine.
type Scheduler struct {
	cfg     Config
	profile Profile
	model   string
	engine  Engine
	cache   Cache
	limits  Limits

	// Set before the start, replaced by the tests.
	now         func() time.Time
	retryPauses []time.Duration
	healthEvery time.Duration
	stopGrace   time.Duration

	// ctx is the context of the requests to the engine; cancel ends
	// them at the end of Stop's grace.
	ctx    context.Context
	cancel context.CancelFunc
	quit   chan struct{}
	// workers are the Slots goroutines that run the items; bg are the
	// health check and the janitor.
	workers sync.WaitGroup
	bg      sync.WaitGroup

	mu      sync.Mutex
	wake    *sync.Cond // a worker may have something to do
	lanes   [priorities]lane
	jobs    map[Key]*job // every job until it finishes
	failed  map[Key]time.Time
	running int
	healthy bool
	// down is closed when the engine becomes unavailable, so that the
	// requests that wait are answered at once; a new one is made when it
	// takes items again.
	down     chan struct{}
	probing  bool
	stopping bool
	itemTime time.Duration // moving average of an item in the engine
}

// StartScheduler starts Slots workers that give the items of the requests
// to engine with profile, write the translations to cache and keep the
// users within limits (nil: no limits). c is a configuration with its
// defaults in, enabled and valid.
func StartScheduler(c Config, profile Profile, engine Engine, cache Cache, limits Limits) (*Scheduler, error) {
	s, err := newScheduler(c, profile, engine, cache, limits)
	if err != nil {
		return nil, err
	}
	s.start()
	return s, nil
}

func newScheduler(c Config, profile Profile, engine Engine, cache Cache, limits Limits) (*Scheduler, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !c.Enabled() {
		return nil, errors.New("translate: a scheduler without an engine")
	}
	if profile == nil || engine == nil || cache == nil {
		return nil, errors.New("translate: a scheduler needs a profile, an engine and a cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{
		cfg:         c,
		profile:     profile,
		model:       c.Model,
		engine:      engine,
		cache:       cache,
		limits:      limits,
		now:         time.Now,
		retryPauses: defaultRetryPauses,
		healthEvery: healthEvery,
		stopGrace:   stopGrace,
		ctx:         ctx,
		cancel:      cancel,
		quit:        make(chan struct{}),
		jobs:        make(map[Key]*job),
		failed:      make(map[Key]time.Time),
		healthy:     true,
		down:        make(chan struct{}),
		itemTime:    initialItemTime,
	}
	s.wake = sync.NewCond(&s.mu)
	return s, nil
}

func (s *Scheduler) start() {
	setEngineUp(true)
	metricQueueDepth.Set(0)
	for range s.cfg.Slots {
		s.workers.Add(1)
		go s.work()
	}
	s.bg.Add(1)
	go s.janitor()
}

// pending is an item of a request that is not answered yet.
type pending struct {
	idx int
	key Key
	src *mtproto.TextWithEntities
}

// Translate translates the items of a, one result for each item, in their
// order. Its errors are those a client is answered with: FLOOD_WAIT_X
// (not ready within the wait, the engine is unavailable, an item failed
// lately, the user is over his limit, bff is stopping),
// INPUT_TEXT_EMPTY, INPUT_TEXT_TOO_LONG and TO_LANG_INVALID (CheckItems
// and the profile), or the error of ctx when the caller went away. It is
// never a failure of the server: a cache that cannot be read is read as
// empty, one that cannot be written loses only that write.
func (s *Scheduler) Translate(ctx context.Context, a Ask) ([]*mtproto.TextWithEntities, error) {
	out, result, err := s.translate(ctx, a)
	metricRequests.Inc(a.Method, result)
	return out, err
}

func (s *Scheduler) translate(ctx context.Context, a Ask) ([]*mtproto.TextWithEntities, string, error) {
	if err := CheckItems(s.cfg, a.Items); err != nil {
		return nil, ResultInvalid, err
	}
	if a.Priority != PriorityManual {
		a.Priority = PriorityChat
	}
	out := make([]*mtproto.TextWithEntities, len(a.Items))
	var (
		todo  []pending
		empty int
	)
	for i, it := range a.Items {
		src, res, err := s.prepare(ctx, a.Lang, it)
		if err != nil {
			return nil, ResultInvalid, err
		}
		if res != nil {
			out[i] = res
			empty++
			continue
		}
		todo = append(todo, pending{idx: i, src: src, key: CacheKey(KeySource{
			Profile:        s.profile.Name(),
			ProfileVersion: s.profile.Version(),
			Model:          s.model,
			ToLang:         a.Lang,
			Text:           src.GetText(),
			Entities:       src.GetEntities(),
		})})
	}

	cached := 0
	if len(todo) > 0 {
		keys := make([]Key, len(todo))
		for i, p := range todo {
			keys[i] = p.key
		}
		found, err := s.cache.CachedTranslations(ctx, keys)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ResultCanceled, ctx.Err()
			}
			// The engine translates them again; their writes may then
			// fail as well, which loses nothing more.
			metricCacheErrors.Inc("read")
			logx.WithContext(ctx).Errorf("translate: %d items of user %d read as not cached: %v", len(keys), a.UserId, err)
		}
		todo = slices.DeleteFunc(todo, func(p pending) bool {
			t, ok := found[p.key]
			if ok {
				out[p.idx] = answerOf(t)
				cached++
			}
			return ok
		})
	}
	if len(todo) == 0 {
		countItems(SourceEmpty, empty)
		countItems(SourceCache, cached)
		return out, ResultOK, nil
	}

	waits, down, failedFor, result, err := s.enqueue(ctx, a, todo)
	if err != nil {
		return nil, result, err
	}
	if failedFor > 0 {
		// The others are queued and go on into the cache meanwhile.
		return nil, ResultFailed, FloodWait(failedFor)
	}

	wait := s.cfg.SyncWait
	if a.Priority == PriorityManual {
		wait = s.cfg.SyncWaitSingle
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
waiting:
	for _, j := range waits {
		select {
		case <-j.done:
		case <-timer.C:
			return nil, ResultWait, FloodWait(s.estimate(a.Priority))
		case <-down:
			if allDone(waits) {
				// Another item took the engine down after these were
				// done.
				break waiting
			}
			// The items wait for the engine and go on into the cache
			// once it is back.
			return nil, ResultDown, FloodWait(engineDownWait)
		case <-ctx.Done():
			return nil, ResultCanceled, ctx.Err()
		}
	}
	for _, j := range waits {
		switch {
		case errors.Is(j.err, errStopped):
			return nil, ResultStopped, FloodWait(engineDownWait)
		case j.err != nil:
			return nil, ResultFailed, FloodWait(j.failedUntil.Sub(s.now()))
		}
	}
	for _, p := range todo {
		out[p.idx] = answerOf(waits[p.key].res)
	}
	countItems(SourceEmpty, empty)
	countItems(SourceCache, cached)
	countItems(SourceEngine, len(todo))
	return out, ResultOK, nil
}

// prepare returns the item it with its entities as the key and the
// markup take them, or its result when it needs no engine: an empty text,
// or no letters out of its verbatim entities.
func (s *Scheduler) prepare(ctx context.Context, lang string, it *mtproto.TextWithEntities) (*mtproto.TextWithEntities, *mtproto.TextWithEntities, error) {
	text := it.GetText()
	if strings.TrimSpace(text) == "" {
		return nil, emptyAnswer(), nil
	}
	ents, err := Normalize(ctx, text, it.GetEntities(), 0, nil)
	if err != nil {
		// Normalize fails only by its UserExistence, and there is none.
		return nil, nil, err
	}
	src := mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text, Entities: ents}).To_TextWithEntities()
	// Translate makes no request for an item that has nothing to
	// translate and gives it back as it is; for any other item the first
	// request is refused here, before anything is sent.
	res, err := Translate(ctx, s.profile, lang, src, func(context.Context, Request) (string, error) {
		return "", errNeedsEngine
	})
	switch {
	case err == nil:
		return nil, mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: res.Text, Entities: res.Entities}).To_TextWithEntities(), nil
	case errors.Is(err, errNeedsEngine):
		return src, nil, nil
	default:
		// ErrToLangInvalid: lang is not of the profile.
		return nil, nil, err
	}
}

// enqueue finds or makes the job of every item of todo. It returns the
// jobs to wait for by key and the signal of the engine becoming
// unavailable, or the longest time an item has still to be answered as
// failed, or the error to answer at once.
func (s *Scheduler) enqueue(ctx context.Context, a Ask, todo []pending) (map[Key]*job, <-chan struct{}, time.Duration, string, error) {
	waits := make(map[Key]*job, len(todo))
	var failedFor time.Duration

	// The new items are counted against the limits of the user out of the
	// lock, which a call to Redis must not hold; an item another request
	// queues meanwhile is joined below, and its count is the only cost.
	s.mu.Lock()
	if result, err := s.refuseLocked(); err != nil {
		s.mu.Unlock()
		return nil, nil, 0, result, err
	}
	now := s.now()
	fresh := 0
	seen := make(map[Key]bool, len(todo))
	for _, p := range todo {
		if seen[p.key] {
			continue
		}
		seen[p.key] = true
		if until, ok := s.failed[p.key]; ok && until.After(now) {
			continue
		}
		if _, ok := s.jobs[p.key]; !ok {
			fresh++
		}
	}
	s.mu.Unlock()

	if fresh > 0 && s.limits != nil {
		if err := s.limits.Reserve(ctx, a.UserId, fresh); err != nil {
			if ctx.Err() != nil {
				return nil, nil, 0, ResultCanceled, ctx.Err()
			}
			return nil, nil, 0, ResultLimited, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if result, err := s.refuseLocked(); err != nil {
		return nil, nil, 0, result, err
	}
	now = s.now()
	queued := false
	for _, p := range todo {
		if _, ok := waits[p.key]; ok {
			continue
		}
		if until, ok := s.failed[p.key]; ok && until.After(now) {
			failedFor = max(failedFor, until.Sub(now))
			continue
		}
		if j, ok := s.jobs[p.key]; ok {
			s.joinLocked(j, a)
			waits[p.key] = j
			continue
		}
		j := &job{
			key:   p.key,
			lang:  a.Lang,
			src:   p.src,
			chars: utf16Len(p.src.GetText()),
			owner: a.UserId,
			prio:  a.Priority,
			state: jobQueued,
			done:  make(chan struct{}),
		}
		s.jobs[p.key] = j
		s.lanes[j.prio].push(j.owner, j, false)
		waits[p.key] = j
		queued = true
	}
	if queued {
		s.wake.Broadcast()
		s.depthLocked()
	}
	return waits, s.down, failedFor, "", nil
}

// refuseLocked returns the answer to a request with items for the engine
// that cannot be queued now.
func (s *Scheduler) refuseLocked() (string, error) {
	switch {
	case s.stopping:
		return ResultStopped, FloodWait(engineDownWait)
	case !s.healthy:
		return ResultDown, FloodWait(engineDownWait)
	}
	return "", nil
}

// joinLocked makes a wait for j: a person who waits for an item queued
// with a batch moves it up to his priority and his turn.
func (s *Scheduler) joinLocked(j *job, a Ask) {
	if a.Priority <= j.prio {
		return
	}
	switch j.state {
	case jobQueued:
		s.lanes[j.prio].remove(j.owner, j)
		j.owner, j.prio = a.UserId, a.Priority
		s.lanes[j.prio].push(j.owner, j, false)
	case jobDelayed:
		j.owner, j.prio = a.UserId, a.Priority
	}
}

// estimate is how long the items of a request of prio may still take: the
// rounds of Slots the items before them and in the engine make.
func (s *Scheduler) estimate(prio Priority) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.healthy {
		return engineDownWait
	}
	ahead := s.running
	for p := prio; p < priorities; p++ {
		ahead += s.lanes[p].n
	}
	rounds := max((ahead+s.cfg.Slots-1)/s.cfg.Slots, 1)
	return time.Duration(rounds) * s.itemTime
}

// work is one slot of the engine: it runs the items one after another.
func (s *Scheduler) work() {
	defer s.workers.Done()
	for {
		j := s.next()
		if j == nil {
			return
		}
		s.run(j)
	}
}

// next waits for an item the engine can take; nil once stopping.
func (s *Scheduler) next() *job {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.stopping {
			return nil
		}
		if s.healthy {
			for p := priorities - 1; p >= 0; p-- {
				if j := s.lanes[p].pop(); j != nil {
					j.state = jobRunning
					s.running++
					return j
				}
			}
		}
		s.wake.Wait()
	}
}

func (s *Scheduler) run(j *job) {
	start := s.now()
	res, err := Translate(s.ctx, s.profile, j.lang, j.src, s.engine.Translate)
	took := s.now().Sub(start)
	if err != nil {
		s.attemptFailed(j, err)
		return
	}
	metricEngineSeconds.ObserveFloat(took.Seconds())
	if res.Fallback {
		metricMarkupFallback.Inc()
	}
	t := &CachedTranslation{Text: res.Text, Entities: res.Entities, Fallback: res.Fallback}
	// The translation goes to the cache before the job ends: a request
	// that comes meanwhile finds the job and waits for it, one that comes
	// later finds the cache.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), putTimeout)
	if err := s.cache.PutTranslation(ctx, j.key, j.lang, s.model, t); err != nil {
		metricCacheErrors.Inc("write")
		logx.WithContext(ctx).Errorf("translate: an item of %d characters to %s is not cached: %v", j.chars, j.lang, err)
	}
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.running--
	if res.Calls > 0 {
		// A moving average over about the last five items.
		s.itemTime = (4*s.itemTime + took) / 5
	}
	if !s.healthy {
		// The engine answered: it takes items again.
		s.upLocked()
	}
	s.finishLocked(j, t, nil)
}

// attemptFailed handles an attempt of j that came to nothing.
func (s *Scheduler) attemptFailed(j *job, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running--
	if s.ctx.Err() != nil {
		// Stop's grace is over; the request was cut by it.
		s.finishLocked(j, nil, errStopped)
		return
	}
	kind := errKind(err)
	metricEngineErrors.Inc(kind)
	switch {
	case errors.Is(err, ErrBusy), errors.Is(err, ErrDown):
		// Not this item's fault: it waits for the engine at the head of
		// its queue, without losing an attempt.
		s.downLocked(err)
		if s.stopping {
			s.finishLocked(j, nil, errStopped)
			return
		}
		j.state = jobQueued
		s.lanes[j.prio].push(j.owner, j, true)
		return
	case errors.Is(err, ErrRejected), errors.Is(err, ErrToLangInvalid):
		// The engine will not take it however often it is sent.
		j.attempt++
		s.failLocked(j, kind, err)
		return
	}
	if errors.Is(err, ErrUnavailable) {
		s.downLocked(err)
	}
	j.attempt++
	if j.attempt > len(s.retryPauses) {
		s.failLocked(j, kind, err)
		return
	}
	if s.stopping {
		s.finishLocked(j, nil, errStopped)
		return
	}
	j.state = jobDelayed
	j.retry = time.AfterFunc(s.retryPauses[j.attempt-1], func() { s.requeue(j) })
}

// requeue puts j back in the queue after its retry pause, at the head of
// the queue of its user: it was asked for before what is behind it.
func (s *Scheduler) requeue(j *job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.finished || j.state != jobDelayed {
		return
	}
	j.retry = nil
	j.state = jobQueued
	s.lanes[j.prio].push(j.owner, j, true)
	s.wake.Broadcast()
}

// failLocked ends j as failed: requests with its item are answered with
// FLOOD_WAIT_X for FailedTTL. Its text is never logged, its length is.
func (s *Scheduler) failLocked(j *job, kind string, err error) {
	until := s.now().Add(s.cfg.FailedTTL)
	s.failed[j.key] = until
	j.failedUntil = until
	logx.Infof("translate: an item of %d characters to %s failed (%s) after %d attempts, refused for %v: %v",
		j.chars, j.lang, kind, j.attempt, s.cfg.FailedTTL, err)
	s.finishLocked(j, nil, fmt.Errorf("translate: the item failed: %w", err))
}

// finishLocked ends j with its translation or its error and wakes the
// requests that wait for it.
func (s *Scheduler) finishLocked(j *job, res *CachedTranslation, err error) {
	if j.finished {
		return
	}
	j.finished = true
	if j.retry != nil {
		j.retry.Stop()
		j.retry = nil
	}
	if s.jobs[j.key] == j {
		delete(s.jobs, j.key)
	}
	j.res, j.err = res, err
	close(j.done)
	s.depthLocked()
}

func (s *Scheduler) depthLocked() { metricQueueDepth.Set(float64(len(s.jobs))) }

// downLocked marks the engine unavailable after err and starts its health
// check.
func (s *Scheduler) downLocked(err error) {
	if !s.healthy {
		return
	}
	s.healthy = false
	close(s.down)
	setEngineUp(false)
	logx.Errorf("translate: the engine is unavailable, new items wait for its health check: %v", err)
	if !s.probing && !s.stopping {
		s.probing = true
		s.bg.Add(1)
		go s.probe()
	}
}

func (s *Scheduler) upLocked() {
	s.healthy = true
	s.down = make(chan struct{})
	setEngineUp(true)
	logx.Info("translate: the engine takes items again")
	s.wake.Broadcast()
}

// probe checks the health of the engine every healthEvery until it
// passes, an item comes back translated, or the scheduler stops. An engine
// without a health check is tried again with the next item after a pause.
func (s *Scheduler) probe() {
	defer s.bg.Done()
	hc, _ := s.engine.(HealthChecker)
	t := time.NewTicker(s.healthEvery)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			s.mu.Lock()
			s.probing = false
			s.mu.Unlock()
			return
		case <-t.C:
		}
		var err error
		if hc != nil {
			ctx, cancel := context.WithTimeout(s.ctx, s.healthEvery)
			err = hc.Health(ctx)
			cancel()
		}
		s.mu.Lock()
		switch {
		case s.healthy:
			// An item came back translated meanwhile.
			s.probing = false
		case err == nil && !s.stopping:
			s.probing = false
			s.upLocked()
		default:
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		return
	}
}

// janitor forgets the expired failures and counts the rows of the cache.
func (s *Scheduler) janitor() {
	defer s.bg.Done()
	failedTick := time.NewTicker(failedSweepEvery)
	defer failedTick.Stop()
	rowsTick := time.NewTicker(cacheRowsEvery)
	defer rowsTick.Stop()
	counter, _ := s.cache.(cacheCounter)
	if counter != nil {
		s.countRows(counter)
	}
	for {
		select {
		case <-s.quit:
			return
		case <-failedTick.C:
			s.mu.Lock()
			now := s.now()
			for k, until := range s.failed {
				if !until.After(now) {
					delete(s.failed, k)
				}
			}
			s.mu.Unlock()
		case <-rowsTick.C:
			if counter != nil {
				s.countRows(counter)
			}
		}
	}
}

func (s *Scheduler) countRows(counter cacheCounter) {
	ctx, cancel := context.WithTimeout(s.ctx, putTimeout)
	defer cancel()
	n, err := counter.CacheRows(ctx)
	if err != nil {
		if s.ctx.Err() == nil {
			metricCacheErrors.Inc("count")
			logx.Errorf("translate: count the cache: %v", err)
		}
		return
	}
	SetCacheRows(n)
}

// Stop stops taking requests, answers the queued items with FLOOD_WAIT_X,
// and waits up to stopGrace for the items in the engine; past it their
// requests are cut. A translation that came back is still written to the
// cache. Stop may be called more than once.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		s.workers.Wait()
		s.bg.Wait()
		return
	}
	s.stopping = true
	close(s.quit)
	for _, j := range s.jobs {
		if j.state == jobQueued {
			s.lanes[j.prio].remove(j.owner, j)
		}
		if j.state != jobRunning {
			s.finishLocked(j, nil, errStopped)
		}
	}
	s.wake.Broadcast()
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.workers.Wait()
		close(done)
	}()
	grace := time.NewTimer(s.stopGrace)
	defer grace.Stop()
	select {
	case <-done:
	case <-grace.C:
		logx.Error("translate: stopped with items still in the engine")
		s.cancel()
		<-done
	}
	s.cancel()
	s.bg.Wait()
}

func allDone(jobs map[Key]*job) bool {
	for _, j := range jobs {
		select {
		case <-j.done:
		default:
			return false
		}
	}
	return true
}

func emptyAnswer() *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "", Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
}

// answerOf returns t as an item of an answer. The entities are copied:
// the job's translation is shared by every request that waited for it.
func answerOf(t *CachedTranslation) *mtproto.TextWithEntities {
	ents := make([]*mtproto.MessageEntity, len(t.Entities))
	for i, e := range t.Entities {
		ents[i] = proto.Clone(e).(*mtproto.MessageEntity)
	}
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: t.Text, Entities: ents}).To_TextWithEntities()
}
