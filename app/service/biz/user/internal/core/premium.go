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

	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// Premium granted by the admin (task 014, ADR 0016). The user's devices
// learn of a change from their own user in the update with seq
// (updates{updateUser, users: [the user]}, ADR 0020): the clients take
// Premium from the premium flag of their user (Android UserConfig ->
// MessagesController.updatePremium).

const (
	// premiumSweep is how often the worker looks for grants whose months
	// are over.
	premiumSweep = time.Minute
	// premiumBatch is the most expiries one sweep handles.
	premiumBatch = 100
	// premiumTimeout bounds one change: the look-up of the user and the box
	// operation.
	premiumTimeout = 2*time.Second + dao.MaxBoxOpTime
	// PremiumStopWait is how long Stop waits for the expiry in progress.
	PremiumStopWait = premiumTimeout
)

var metricPremiumErrors = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "user",
	Name:      "premium_expiry_errors_total",
	Help:      "Failed expiries of Premium grants; the worker retries them.",
	Labels:    []string{},
})

// ChangePremium changes userId's Premium and tells the user's devices when
// it changed.
func ChangePremium(ctx context.Context, svcCtx *svc.ServiceContext, userId int64, change dao.PremiumChange) (dao.PremiumState, error) {
	ctx, cancel := context.WithTimeout(ctx, premiumTimeout)
	defer cancel()
	state, _, err := svcCtx.Dao.ChangePremium(ctx, userId, change)
	return state, err
}

// PremiumWorker takes Premium away from the users whose months are over and
// tells their devices. Several workers (biz processes) may run: a change is
// one box transaction of the user, and an expired grant is gone after it.
type PremiumWorker struct {
	svcCtx *svc.ServiceContext
	stop   chan struct{}
	done   chan struct{}
	start  sync.Once
	halt   sync.Once
}

func NewPremiumWorker(svcCtx *svc.ServiceContext) *PremiumWorker {
	return &PremiumWorker{svcCtx: svcCtx, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start runs the worker until Stop.
func (w *PremiumWorker) Start() {
	w.start.Do(func() { go w.run() })
}

// Stop stops the worker and waits for the expiry in progress, at most
// PremiumStopWait; one cut short is rolled back and done by the next sweep.
func (w *PremiumWorker) Stop() {
	w.halt.Do(func() { close(w.stop) })
	w.start.Do(func() { close(w.done) })
	select {
	case <-w.done:
	case <-time.After(PremiumStopWait):
		logx.Errorf("user: Premium expiry still running after %v", PremiumStopWait)
	}
}

func (w *PremiumWorker) run() {
	defer close(w.done)
	ticker := time.NewTicker(premiumSweep)
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

func (w *PremiumWorker) sweep(ctx context.Context) {
	ids, err := w.svcCtx.Dao.ExpiredPremium(ctx, time.Now().Unix(), premiumBatch)
	if err != nil {
		metricPremiumErrors.Inc()
		logx.WithContext(ctx).Errorf("user: expired Premium grants: %v", err)
		return
	}
	for _, id := range ids {
		select {
		case <-w.stop:
			return
		default:
		}
		if _, err := ChangePremium(ctx, w.svcCtx, id, dao.PremiumChange{OnlyExpired: true}); err != nil {
			metricPremiumErrors.Inc()
			logx.WithContext(ctx).Errorf("user: Premium expiry of user %d: %v", id, err)
		}
	}
}

// Workers are the background jobs of the user service, which the server
// starts and stops with itself.
type Workers struct {
	SignUps *SignUpWorker
	Premium *PremiumWorker
}

// Start starts every worker.
func (w *Workers) Start() {
	w.SignUps.Start()
	w.Premium.Start()
}

// Stop stops every worker; they finish their work in progress together.
func (w *Workers) Stop() {
	var wg sync.WaitGroup
	for _, stop := range []func(){w.SignUps.Stop, w.Premium.Stop} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stop()
		}()
	}
	wg.Wait()
}
