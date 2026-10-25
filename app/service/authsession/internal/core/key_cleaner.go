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

package core

import (
	"context"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/service/authsession/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

const (
	// keyCleanerSweep is how often expired temporary keys are removed.
	keyCleanerSweep = 10 * time.Minute
	// keyCleanerBatch is the most keys one round of a sweep removes; a sweep
	// runs rounds until none are left.
	keyCleanerBatch = 500
	// KeyCleanerStopWait is how long Stop waits for the round in progress.
	KeyCleanerStopWait = 10 * time.Second
)

var metricKeyCleanerErrors = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "authsession",
	Name:      "key_cleaner_errors_total",
	Help:      "Failed removals of expired temporary auth keys and sealings of plain ones; the next sweep retries them.",
	Labels:    []string{},
})

// KeyCleaner removes temporary keys whose lifetime is over (task 008): an
// expired key already gets AUTH_KEY_UNREGISTERED, its rows only linger. It
// also seals, in rounds, the plain keys stored before task 008 that no
// client has used since (a used one is sealed on its first read).
// Several authsession processes may run one each: a removal is one
// transaction, and a key removed twice is removed once.
type KeyCleaner struct {
	svcCtx *svc.ServiceContext
	stop   chan struct{}
	done   chan struct{}
	start  sync.Once
	halt   sync.Once
}

func NewKeyCleaner(svcCtx *svc.ServiceContext) *KeyCleaner {
	return &KeyCleaner{svcCtx: svcCtx, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start runs the cleaner until Stop.
func (w *KeyCleaner) Start() {
	w.start.Do(func() { go w.run() })
}

// Stop stops the cleaner and waits for the round in progress, at most
// KeyCleanerStopWait.
func (w *KeyCleaner) Stop() {
	w.halt.Do(func() { close(w.stop) })
	w.start.Do(func() { close(w.done) })
	select {
	case <-w.done:
	case <-time.After(KeyCleanerStopWait):
		logx.Errorf("authsession: removal of expired keys still running after %v", KeyCleanerStopWait)
	}
}

func (w *KeyCleaner) run() {
	defer close(w.done)
	ticker := time.NewTicker(keyCleanerSweep)
	defer ticker.Stop()
	for {
		w.sweep(context.Background())
		select {
		case <-w.stop:
			return
		case <-ticker.C:
		}
	}
}

// sweep removes the temporary keys expired at its start, then seals plain
// keys.
func (w *KeyCleaner) sweep(ctx context.Context) {
	w.removeExpired(ctx)
	w.sealPlain(ctx)
}

func (w *KeyCleaner) removeExpired(ctx context.Context) {
	now := time.Now().Unix()
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		ids, err := w.svcCtx.Dao.ExpiredTempAuthKeys(ctx, now, keyCleanerBatch)
		if err != nil {
			metricKeyCleanerErrors.Inc()
			logx.WithContext(ctx).Errorf("authsession: expired temporary keys: %v", err)
			return
		}
		if len(ids) == 0 {
			return
		}
		if _, err = w.svcCtx.Dao.DeleteAuthKeys(ctx, ids); err != nil {
			metricKeyCleanerErrors.Inc()
			logx.WithContext(ctx).Errorf("authsession: remove %d expired temporary keys: %v", len(ids), err)
			return
		}
		logx.WithContext(ctx).Infof("authsession: removed %d expired temporary keys", len(ids))
		if len(ids) < keyCleanerBatch {
			return
		}
	}
}

func (w *KeyCleaner) sealPlain(ctx context.Context) {
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		n, err := w.svcCtx.Dao.SealPlainAuthKeys(ctx, keyCleanerBatch)
		if err != nil {
			metricKeyCleanerErrors.Inc()
			logx.WithContext(ctx).Errorf("authsession: seal plain keys: %v", err)
			return
		}
		if n > 0 {
			logx.WithContext(ctx).Infof("authsession: sealed %d plain keys", n)
		}
		if n < keyCleanerBatch {
			return
		}
	}
}
