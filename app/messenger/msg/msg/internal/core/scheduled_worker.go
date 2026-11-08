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
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/svc"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// scheduledTick is how often the worker looks for due messages, so a
	// message goes out within about a second of its date.
	scheduledTick = time.Second
	// scheduledOnlineTick is how often it looks for messages waiting for a
	// peer to come online, which asks the user service about each peer.
	scheduledOnlineTick = 5 * time.Second
)

// ScheduledWorker delivers the messages of the schedule queues. Any number
// of instances may run: a worker claims rows before delivering them, and a
// delivery deletes its rows in the transaction that sends the messages, so
// a message is sent once whatever crashes when. A delivery that fails on a
// passing error keeps its rows and is tried again when the lease runs out;
// one that can never go out is dropped and the sender is told (dropScheduled).
type ScheduledWorker struct {
	svcCtx *svc.ServiceContext
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduledWorker returns a stopped worker over svcCtx.
func NewScheduledWorker(svcCtx *svc.ServiceContext) *ScheduledWorker {
	return &ScheduledWorker{svcCtx: svcCtx}
}

// Start runs the worker until Stop.
func (w *ScheduledWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()
}

// Stop stops the worker and waits for the delivery in progress.
func (w *ScheduledWorker) Stop() {
	if w.cancel == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
}

func (w *ScheduledWorker) run(ctx context.Context) {
	ticks := time.NewTicker(scheduledTick)
	defer ticks.Stop()
	next := time.Time{}
	for {
		w.deliverDue(ctx)
		if now := time.Now(); !now.Before(next) {
			next = now.Add(scheduledOnlineTick)
			w.deliverWhenOnline(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks.C:
		}
	}
}

func (w *ScheduledWorker) deliverDue(ctx context.Context) {
	rows, err := w.svcCtx.Dao.UnclaimedDue(ctx, time.Now().Unix(), scheduledBatch)
	if err != nil {
		if ctx.Err() == nil {
			logx.Errorf("msg: read due scheduled messages: %v", err)
		}
		return
	}
	done := map[int32]bool{}
	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		w.deliverRow(ctx, r, done)
	}
}

// deliverWhenOnline sends the messages waiting for peers that are online
// now. The status is the one users see (user.getLastSeens): a peer comes
// online when its client says so, account.updateStatus.
func (w *ScheduledWorker) deliverWhenOnline(ctx context.Context) {
	done := map[int32]bool{}
	for afterId := int32(0); ctx.Err() == nil; {
		now := time.Now().Unix()
		rows, err := w.svcCtx.Dao.UnclaimedWhenOnline(ctx, now, afterId, scheduledBatch)
		if err != nil {
			if ctx.Err() == nil {
				logx.Errorf("msg: read scheduled messages waiting for a peer: %v", err)
			}
			return
		}
		if len(rows) == 0 || !w.deliverToOnlinePeers(ctx, rows, now, done) || len(rows) < scheduledBatch {
			return
		}
		afterId = rows[len(rows)-1].Id
	}
}

// deliverToOnlinePeers delivers those of rows whose peer is online at now.
// false means the statuses could not be read.
func (w *ScheduledWorker) deliverToOnlinePeers(ctx context.Context, rows []*dao.ScheduledRow, now int64, done map[int32]bool) bool {
	peers := make([]int64, 0, len(rows))
	for _, r := range rows {
		if !slices.Contains(peers, r.PeerId) {
			peers = append(peers, r.PeerId)
		}
	}
	seen, err := w.svcCtx.Dao.UserClient.UserGetLastSeens(ctx, &userpb.TLUserGetLastSeens{Id: peers})
	if err != nil {
		if ctx.Err() == nil {
			logx.Errorf("msg: read the status of scheduled peers: %v", err)
		}
		return false
	}
	online := map[int64]bool{}
	for _, d := range seen.GetDatas() {
		online[d.GetUserId()] = d.GetLastSeenAt()+int64(d.GetExpires()) > now
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			return false
		}
		if online[r.PeerId] {
			w.deliverRow(ctx, r, done)
		}
	}
	return true
}

// deliverRow claims r, with the rest of its album, and delivers it.
func (w *ScheduledWorker) deliverRow(ctx context.Context, r *dao.ScheduledRow, done map[int32]bool) {
	if done[r.Id] {
		return
	}
	now := time.Now().Unix()
	unit := []*dao.ScheduledRow{r}
	if r.GroupedId != 0 {
		album, err := w.svcCtx.Dao.UnclaimedAlbum(ctx, r, now)
		if err != nil {
			logx.Errorf("msg: read an album of a scheduled message: %v", err)
			return
		}
		if len(album) != 0 {
			unit = album
		}
	}
	for _, it := range unit {
		done[it.Id] = true
	}
	ids := dao.ScheduledIds(unit)
	claimed, err := w.svcCtx.Dao.ClaimScheduled(ctx, ids, now, now+int64(dao.ScheduledLease/time.Second))
	if err != nil {
		logx.Errorf("msg: claim scheduled messages: %v", err)
		return
	}
	if !claimed {
		return
	}
	// A delivery goes on through a stop of the worker: it ends well before
	// the lease does.
	deliverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dao.ScheduledLease/2)
	defer cancel()
	// An edit committed between the read and the claim counts: from the
	// claim on an edit is refused (editScheduled), so these are final.
	fresh, err := w.svcCtx.Dao.LoadScheduledRows(deliverCtx, ids)
	if err != nil {
		logx.Errorf("msg: read claimed scheduled messages: %v", err)
		return
	}
	if len(fresh) == 0 {
		return
	}
	if rescheduled(unit, fresh) {
		if err = w.svcCtx.Dao.ReleaseScheduled(deliverCtx, ids); err != nil {
			logx.Errorf("msg: release scheduled messages %v: %v", ids, err)
		}
		return
	}
	unit, ids = fresh, dao.ScheduledIds(fresh)
	c := New(deliverCtx, w.svcCtx)
	if _, err = c.deliverScheduled(unit, 0, true); err == nil {
		return
	}
	switch {
	case errors.Is(err, dao.ErrScheduledGone):
		// Deleted, or sent by hand, after it was read.
	case permanentSendError(err):
		logx.Errorf("msg: scheduled messages %v of user %d cannot be sent, dropped: %v", ids, unit[0].SenderUserId, err)
		if err = c.dropScheduled(unit); err != nil && !errors.Is(err, dao.ErrScheduledGone) {
			logx.Errorf("msg: drop scheduled messages %v: %v", ids, err)
		}
	default:
		logx.Errorf("msg: deliver scheduled messages %v: %v", ids, err)
	}
}

// rescheduled tells whether an edit moved one of the rows read before a
// claim to another date, which is then the next tick's to judge.
func rescheduled(before, fresh []*dao.ScheduledRow) bool {
	dates := make(map[int32]int32, len(before))
	for _, r := range before {
		dates[r.Id] = r.ScheduleDate
	}
	return slices.ContainsFunc(fresh, func(r *dao.ScheduledRow) bool {
		return r.ScheduleDate != dates[r.Id]
	})
}
