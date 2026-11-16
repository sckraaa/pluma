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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

const (
	// autoReplyTick is how often the worker looks for queued replies: a
	// reply goes about a second after the message that triggered it.
	autoReplyTick = time.Second
	// autoReplyBatch is how many queued replies one look takes.
	autoReplyBatch = 100
)

// The automatic replies sent, given up or found not to go, by kind
// (greeting, away) and result (sent, skipped, failed): the alert
// BusinessAutoReplyFailing watches the share of failed.
var metricAutoReplies = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "business",
	Name:      "autoreply_total",
	Help:      "Automatic replies of Telegram Business by kind and result.",
	Labels:    []string{"kind", "result"},
})

// AutoReplyWorker sends the automatic replies of Telegram Business that
// the inbox queued (business_auto_replies, task 105): the messages of the
// shortcut of the reply, from the account to the user who wrote, as the
// account sends a shortcut (sendQuickReplies), marked offline.
//
// Any number of instances may run. A worker claims a row for
// business.AutoReplyLease before it sends it, and the send deletes the row
// in the account's box transaction that writes the messages, so a reply
// goes once whatever crashes when: a row whose worker died is claimed again
// when its lease ends, and its random ids, derived from the row
// (autoReplyRandomIds), find the messages if they went before the crash.
// A reply that can never go (the setting is off, the user blocked the
// account, its privacy or the shortcut refuse it) is dropped; one that
// fails on a passing error is tried again after the lease, at most
// business.AutoReplyAttemptsMax times.
type AutoReplyWorker struct {
	svcCtx *svc.ServiceContext
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAutoReplyWorker returns a stopped worker over svcCtx.
func NewAutoReplyWorker(svcCtx *svc.ServiceContext) *AutoReplyWorker {
	return &AutoReplyWorker{svcCtx: svcCtx}
}

// Start runs the worker until Stop.
func (w *AutoReplyWorker) Start() {
	// Every series starts at zero: increase() takes no count from the
	// first sample of a series, so a counter born at 1 by the first failure
	// would never show it to BusinessAutoReplyFailing.
	for _, kind := range []business.AutoReplyKind{business.KindGreeting, business.KindAway} {
		for _, outcome := range []autoReplyOutcome{autoReplySent, autoReplySkipped, autoReplyFailed} {
			metricAutoReplies.Add(0, kind.String(), string(outcome))
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()
}

// Stop stops the worker and waits for the send in progress.
func (w *AutoReplyWorker) Stop() {
	if w.cancel == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
}

func (w *AutoReplyWorker) run(ctx context.Context) {
	ticks := time.NewTicker(autoReplyTick)
	defer ticks.Stop()
	for {
		w.sendQueued(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticks.C:
		}
	}
}

// sendQueued sends the queued replies no worker holds now.
func (w *AutoReplyWorker) sendQueued(ctx context.Context) {
	rows, err := w.svcCtx.Dao.Business().UnclaimedAutoReplies(ctx, time.Now().UnixMilli(), autoReplyBatch)
	if err != nil {
		if ctx.Err() == nil {
			logx.Errorf("msg: read queued automatic replies: %v", err)
		}
		return
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		w.sendRow(ctx, r.ID)
	}
}

// autoReplyOutcome is what became of a queued reply.
type autoReplyOutcome string

const (
	autoReplySent    autoReplyOutcome = "sent"
	autoReplySkipped autoReplyOutcome = "skipped"
	autoReplyFailed  autoReplyOutcome = "failed"
	// autoReplyRetry: a passing error; the row stays for another attempt.
	autoReplyRetry autoReplyOutcome = ""
)

// sendRow claims the queued reply id and sends it.
func (w *AutoReplyWorker) sendRow(ctx context.Context, id int64) {
	store := w.svcCtx.Dao.Business()
	now := time.Now()
	row, err := store.ClaimAutoReply(ctx, id, now.UnixMilli(), now.Add(business.AutoReplyLease).UnixMilli())
	if err != nil {
		logx.Errorf("msg: claim automatic reply %d: %v", id, err)
		return
	}
	if row == nil {
		return
	}
	if row.Attempts > business.AutoReplyAttemptsMax {
		logx.Errorf("msg: automatic reply %d of user %d given up after %d attempts", row.ID, row.UserID, row.Attempts-1)
		w.drop(ctx, row, autoReplyFailed)
		return
	}
	// A send goes on through a stop of the worker: it ends well before the
	// lease does.
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), business.AutoReplyLease/2)
	defer cancel()
	switch outcome := New(sendCtx, w.svcCtx).sendAutoReply(row); outcome {
	case autoReplySent:
		metricAutoReplies.Inc(row.Kind.String(), string(autoReplySent))
	case autoReplyRetry:
	default:
		w.drop(sendCtx, row, outcome)
	}
}

// drop takes row off the queue unsent and counts it.
func (w *AutoReplyWorker) drop(ctx context.Context, row *business.QueuedAutoReply, outcome autoReplyOutcome) {
	if err := w.svcCtx.Dao.Business().DropAutoReply(ctx, row.ID); err != nil {
		logx.Errorf("msg: drop automatic reply %d: %v", row.ID, err)
		return
	}
	metricAutoReplies.Inc(row.Kind.String(), string(outcome))
}

// sendAutoReply sends row: the shortcut the setting of its kind names now,
// to the private chat with the user who wrote. The setting may have changed
// since the row was queued: turned off, the row goes unsent; another
// shortcut, that one goes.
func (c *MsgCore) sendAutoReply(row *business.QueuedAutoReply) autoReplyOutcome {
	settings, err := c.svcCtx.Dao.Business().AutoReplies(c.ctx, row.UserID)
	if err != nil {
		c.Logger.Errorf("msg: automatic reply %d: read the settings: %v", row.ID, err)
		return autoReplyRetry
	}
	setting := business.AutoReplyOf(settings, row.Kind)
	if setting == nil {
		return autoReplySkipped
	}
	// A user who blocked the account gets nothing from it; the ordinary
	// send would keep the messages in the account's chat alone.
	blocked, err := c.isBlockedBy(row.UserID, row.PeerID)
	if err != nil {
		c.Logger.Errorf("msg: automatic reply %d: read the block: %v", row.ID, err)
		return autoReplyRetry
	}
	if blocked {
		return autoReplySkipped
	}
	_, err = c.sendQuickReplies(quickReplySend{
		userId:       row.UserID,
		peerId:       row.PeerID,
		shortcutId:   setting.ShortcutID,
		randomIdsFor: func(n int) []int64 { return autoReplyRandomIds(row.ID, n) },
		offline:      true,
		opt: sendOptions{after: func(b *userbox.Box, _ []int32) error {
			return business.FinishAutoReplyTx(b.Tx(), row, int32(b.Date()))
		}},
	})
	switch {
	case err == nil:
		return autoReplySent
	case errors.Is(err, business.ErrAutoReplyGone):
		// The setting was turned off after the look above; the send
		// rolled back with the transaction.
		return autoReplySkipped
	case permanentSendError(err):
		c.Logger.Errorf("msg: automatic reply %d of user %d cannot be sent, dropped: %s", row.ID, row.UserID, status.Convert(err).Message())
		return autoReplySkipped
	}
	c.Logger.Errorf("msg: automatic reply %d of user %d, attempt %d: %v", row.ID, row.UserID, row.Attempts, err)
	return autoReplyRetry
}

// autoReplyRandomIds are the random ids of the n messages of the queued
// reply id: the same for every attempt, so that the send of an attempt
// after a crash finds the messages an earlier one wrote (SentBefore), and
// apart from any other reply's. A random id is unique per sender; 63 bits
// of a hash do not meet the client's ones.
func autoReplyRandomIds(id int64, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		sum := sha256.Sum256([]byte("business-auto-reply:" + strconv.FormatInt(id, 10) + ":" + strconv.Itoa(i)))
		v := int64(binary.BigEndian.Uint64(sum[:8]) &^ (1 << 63))
		if v == 0 {
			v = 1
		}
		out[i] = v
	}
	return out
}
