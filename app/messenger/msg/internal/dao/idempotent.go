package dao

import (
	"context"
	"fmt"
	"time"

	"github.com/teamgram/marmota/pkg/idempotent"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// The idempotency window of a send until task 003 (ADR 0011, p. 6): a
// request repeated with the same random_id while the reply is cached gets
// the cached reply instead of a second message. Redis is a cache here: when
// it fails, the send goes on without the window.
const (
	// sendOnceSection is the longest a request holds the idempotency lock:
	// the box operation with the publication of its events (MaxBoxOpTime)
	// and caching its reply. Every Redis step is limited by cacheTimeout.
	sendOnceSection = MaxBoxOpTime + cacheTimeout
	// IdempotencyLockTTL outlives sendOnceSection, so a second request
	// cannot start a second transaction while the first one still runs.
	IdempotencyLockTTL = 60 * time.Second
	// idempotencyReplyTTL is how long the reply stays cached, in seconds.
	idempotencyReplyTTL = 90
	// idempotencyPoll is how often a request waiting for the one holding the
	// lock looks for its reply.
	idempotencyPoll = 100 * time.Millisecond
	// MaxRequestTime bounds a request with a box operation: the Redis steps
	// of the window (lookup, lock, reply, unlock) and the box operation.
	// RPCs to other services before it are not counted.
	MaxRequestTime = 2*cacheTimeout + sendOnceSection + cacheTimeout
)

// idempotentStore is the Redis side of the window (marmota idempotent).
type idempotentStore interface {
	TryGetCacheValue(ctx context.Context, v any) (bool, error)
	Lock(ctx context.Context, seconds int) (bool, error)
	Unlock(ctx context.Context) error
	SetCacheValue(ctx context.Context, v any, expired int) error
}

func (d *Dao) idempotentStore(key string) idempotentStore {
	if d.idempotent != nil {
		return d.idempotent(key)
	}
	return idempotent.NewIdempotent(d.Redis, key)
}

// SendOnce runs send — a box operation (RunBox) that fills reply — at most
// once per key of senderUserId while the reply is cached:
//
//	cached reply → reply as cached, send not called;
//	lock taken   → reply cached meanwhile? as cached : send (commit,
//	               publication) → reply cached → lock released;
//	lock held    → waits for the reply of the request holding it, or for
//	               the lock, if that request failed.
//
// After commit, a failure to cache the reply is logged and counted, never
// returned: an error would make the client repeat the request and send a
// second message. Redis failing on the cache read or the lock does not stop
// the send: it goes on without the window and is counted. Until task 003, a
// process that stops between the publication and caching the reply leaves
// the send to be repeated once the lock expires.
func (d *Dao) SendOnce(ctx context.Context, senderUserId int64, key string, reply any, send func(ctx context.Context) error) error {
	st := d.idempotentStore(fmt.Sprintf("idempotent#%d@%s", senderUserId, key))
	log := logx.WithContext(ctx)
	redisCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), cacheTimeout)
	}
	cached := func() (bool, error) {
		cctx, cancel := redisCtx()
		defer cancel()
		return st.TryGetCacheValue(cctx, reply)
	}

	deadline := time.Now().Add(IdempotencyLockTTL)
	for {
		found, err := cached()
		if err != nil {
			metricIdempotencyBypass.Inc()
			log.Errorf("msg: idempotency cache of user %d unavailable: %v", senderUserId, err)
			return send(ctx)
		}
		if found {
			return nil
		}

		lctx, cancel := redisCtx()
		locked, err := st.Lock(lctx, int(IdempotencyLockTTL/time.Second))
		cancel()
		if err != nil {
			metricIdempotencyBypass.Inc()
			log.Errorf("msg: idempotency lock of user %d unavailable: %v", senderUserId, err)
			return send(ctx)
		}
		if locked {
			break
		}
		// Another request with the same key is running.
		if time.Now().After(deadline) {
			return userbox.ErrCommitUnknown
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(idempotencyPoll):
		}
	}
	unlock := func() {
		uctx, cancel := redisCtx()
		defer cancel()
		if err := st.Unlock(uctx); err != nil {
			log.Errorf("msg: release the idempotency lock of user %d: %v", senderUserId, err)
		}
	}

	// The request that held the lock may have cached its reply and released
	// the lock between the lookup and the lock above. If the cache cannot
	// be read now, the lock still keeps a repeated request out.
	found, err := cached()
	if err != nil {
		log.Errorf("msg: idempotency cache of user %d unavailable: %v", senderUserId, err)
	}
	if found {
		unlock()
		return nil
	}

	err = send(ctx)
	if err == nil {
		cctx, cancel := redisCtx()
		if cerr := st.SetCacheValue(cctx, reply, idempotencyReplyTTL); cerr != nil {
			metricPostCommitErrors.Inc("idempotency")
			log.Errorf("msg: cache the reply of user %d: %v", senderUserId, cerr)
		}
		cancel()
	}
	unlock()
	return err
}
