package dao

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// memStore is an idempotentStore in memory, with injectable failures.
type memStore struct {
	mu     sync.Mutex
	value  []byte
	locked bool
	// beforeLock runs once in Lock, before the lock is tried: what another
	// request does between the cache lookup and the lock.
	beforeLock func(s *memStore)
	getErr     error
	lockErr    error
	setErr     error
	sets       int
}

func (s *memStore) TryGetCacheValue(_ context.Context, v any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return false, s.getErr
	}
	if s.value == nil {
		return false, nil
	}
	return true, json.Unmarshal(s.value, v)
}

func (s *memStore) Lock(context.Context, int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lockErr != nil {
		return false, s.lockErr
	}
	if f := s.beforeLock; f != nil {
		s.beforeLock = nil
		f(s)
	}
	if s.locked {
		return false, nil
	}
	s.locked = true
	return true, nil
}

func (s *memStore) Unlock(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locked = false
	return nil
}

func (s *memStore) SetCacheValue(_ context.Context, v any, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	if s.setErr != nil {
		return s.setErr
	}
	var err error
	s.value, err = json.Marshal(v)
	return err
}

func daoWith(st *memStore) *Dao {
	return &Dao{idempotent: func(string) idempotentStore { return st }}
}

// sendFn counts its calls and fills the reply.
func sendFn(calls *int, reply *string, value string, err error) func(context.Context) error {
	return func(context.Context) error {
		*calls++
		if err != nil {
			return err
		}
		*reply = value
		return nil
	}
}

func TestSendOnce(t *testing.T) {
	redisDown := errors.New("redis down")
	t.Run("first send runs and caches the reply", func(t *testing.T) {
		st := &memStore{}
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r1", nil))
		if err != nil || calls != 1 || reply != "r1" || string(st.value) != `"r1"` || st.locked {
			t.Fatalf("err %v, calls %d, reply %q, cached %s, locked %v", err, calls, reply, st.value, st.locked)
		}
	})
	t.Run("repeat gets the cached reply", func(t *testing.T) {
		st := &memStore{value: []byte(`"r1"`)}
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r2", nil))
		if err != nil || calls != 0 || reply != "r1" {
			t.Fatalf("err %v, calls %d, reply %q", err, calls, reply)
		}
	})
	t.Run("caching fails after commit: success", func(t *testing.T) {
		st := &memStore{setErr: redisDown}
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r1", nil))
		if err != nil || calls != 1 || reply != "r1" || st.locked {
			t.Fatalf("err %v, calls %d, reply %q, locked %v", err, calls, reply, st.locked)
		}
	})
	t.Run("redis down on the cache read: send without the window", func(t *testing.T) {
		st := &memStore{getErr: redisDown}
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r1", nil))
		if err != nil || calls != 1 {
			t.Fatalf("err %v, calls %d", err, calls)
		}
	})
	t.Run("redis down on the lock: send without the window", func(t *testing.T) {
		st := &memStore{lockErr: redisDown}
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r1", nil))
		if err != nil || calls != 1 {
			t.Fatalf("err %v, calls %d", err, calls)
		}
	})
	t.Run("failed send: nothing cached, lock released", func(t *testing.T) {
		st := &memStore{}
		var (
			reply string
			calls int
		)
		failed := errors.New("MSG_ID_INVALID")
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "", failed))
		if !errors.Is(err, failed) || st.value != nil || st.locked || st.sets != 0 {
			t.Fatalf("err %v, cached %s, locked %v, sets %d", err, st.value, st.locked, st.sets)
		}
	})
	t.Run("reply cached between the lookup and the lock", func(t *testing.T) {
		// The request holding the lock caches its reply and releases the
		// lock right after this one found no reply.
		st := &memStore{locked: true, beforeLock: func(s *memStore) {
			s.value, s.locked = []byte(`"r1"`), false
		}}
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r2", nil))
		if err != nil || calls != 0 || reply != "r1" || st.locked {
			t.Fatalf("err %v, calls %d, reply %q, locked %v", err, calls, reply, st.locked)
		}
	})
	t.Run("concurrent repeat waits for the reply", func(t *testing.T) {
		st := &memStore{locked: true}
		go func() {
			time.Sleep(3 * idempotencyPoll)
			_ = st.SetCacheValue(context.Background(), "r1", 0)
			_ = st.Unlock(context.Background())
		}()
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r2", nil))
		if err != nil || calls != 0 || reply != "r1" {
			t.Fatalf("err %v, calls %d, reply %q", err, calls, reply)
		}
	})
	t.Run("concurrent repeat runs when the first failed", func(t *testing.T) {
		st := &memStore{locked: true}
		go func() {
			time.Sleep(3 * idempotencyPoll)
			_ = st.Unlock(context.Background())
		}()
		var (
			reply string
			calls int
		)
		err := daoWith(st).SendOnce(context.Background(), 1, "k", &reply, sendFn(&calls, &reply, "r2", nil))
		if err != nil || calls != 1 || reply != "r2" {
			t.Fatalf("err %v, calls %d, reply %q", err, calls, reply)
		}
	})
}

// The lock of a send outlives everything a request does under it, each step
// at its limit: so a repeated request cannot start a second transaction
// while the first one still runs. Raising a limit of userbox (Budget,
// PublishTimeout) or cacheTimeout without the lock fails here.
func TestIdempotencyLockOutlivesSection(t *testing.T) {
	underLock := cacheTimeout + // cache deletion before the transaction
		userbox.MaxRunTime + // the transaction with its retries and commit check
		userbox.PublishTimeout + // the publication of its events
		cacheTimeout + // cache deletion after commit
		cacheTimeout // caching the reply
	if sendOnceSection < underLock {
		t.Fatalf("sendOnceSection %v, the steps under the lock take up to %v", sendOnceSection, underLock)
	}
	if margin := 5 * time.Second; IdempotencyLockTTL < sendOnceSection+margin {
		t.Fatalf("lock TTL %v, want at least %v + %v", IdempotencyLockTTL, sendOnceSection, margin)
	}
	if MaxRequestTime < sendOnceSection+3*cacheTimeout {
		t.Fatalf("MaxRequestTime %v does not cover the lookup, the lock and the unlock around %v", MaxRequestTime, sendOnceSection)
	}
}
