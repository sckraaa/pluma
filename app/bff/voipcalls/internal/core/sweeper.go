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

	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	sweepTick = 2 * time.Second
	// sweepBatch bounds one pass: a backlog only shortens the next pass.
	sweepBatch = 64
	// keepEnded is how long an ended call stays for ratings and debug logs.
	keepEnded = 30 * 24 * time.Hour
	// giveUpPosting: after this long the service message of a call is not
	// retried.
	giveUpPosting = time.Hour
)

// Sweeper ends the calls their clients abandoned and finishes the history
// of those whose message was lost. It holds no state of its own: everything
// is in the table, so a restart or a second instance changes nothing.
type Sweeper struct {
	svcCtx *svc.ServiceContext
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StartSweeper starts the sweep over svcCtx.
func StartSweeper(svcCtx *svc.ServiceContext) *Sweeper {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sweeper{svcCtx: svcCtx, cancel: cancel}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(sweepTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.pass(ctx)
			}
		}
	}()
	return s
}

// Stop stops the sweep and waits for the pass in progress.
func (s *Sweeper) Stop() {
	s.cancel()
	s.wg.Wait()
}

// expiryReason is the reason of a call that timed out: unanswered is missed,
// a call lost during the key exchange or later is a failure.
func expiryReason(st phonecalls.State) phonecalls.Reason {
	if st == phonecalls.StateRequested {
		return phonecalls.ReasonMissed
	}
	return phonecalls.ReasonDisconnect
}

func (s *Sweeper) pass(ctx context.Context) {
	c := &VoipCallsCore{ctx: ctx, svcCtx: s.svcCtx, Logger: logx.WithContext(ctx)}
	due, err := s.svcCtx.Dao.Store.Due(ctx, sweepBatch)
	if err != nil {
		logx.WithContext(ctx).Errorf("calls: expired calls: %v", err)
	}
	for _, call := range due {
		wasRequested := call.State == phonecalls.StateRequested
		updated, won, err := s.svcCtx.Dao.Store.Discard(ctx, call.Id, expiryReason(call.State))
		if err != nil {
			logx.WithContext(ctx).Errorf("calls: end call %d: %v", call.Id, err)
			continue
		}
		if won {
			c.finish(updated, wasRequested, 0, 0)
		}
	}
	lost, err := s.svcCtx.Dao.Store.Unposted(ctx, time.Now().Add(-5*time.Second), sweepBatch)
	if err != nil {
		logx.WithContext(ctx).Errorf("calls: unposted calls: %v", err)
	}
	for _, call := range lost {
		if time.Now().Unix()-int64(call.EndDate) > int64(giveUpPosting/time.Second) {
			// A message that cannot go out (a user gone) must not be tried
			// forever.
			logx.WithContext(ctx).Errorf("calls: the history message of call %d given up", call.Id)
			if err := s.svcCtx.Dao.Store.MarkPosted(ctx, call.Id); err != nil {
				logx.WithContext(ctx).Errorf("calls: posting of call %d: %v", call.Id, err)
			}
			continue
		}
		c.postMessage(call)
	}
	if _, err := s.svcCtx.Dao.Store.Purge(ctx, time.Now().Add(-keepEnded), sweepBatch); err != nil {
		logx.WithContext(ctx).Errorf("calls: purge: %v", err)
	}
}
