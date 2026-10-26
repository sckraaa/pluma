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

package dao

import (
	"context"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/session"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Delivery of pushes from sync to the session nodes (ADR 0012, p. 7).
//
// A push of one Sync-T record goes to all target devices in parallel, and
// the consumer marks the record's offset only when every device is done:
// delivered, dropped for good, or marked for resync. Records of one user
// come from one partition in order, so pushes reach each device in order
// without per-device queues. A device that missed a push is marked for
// resync and gets one updatesTooLong, after which the client catches up with
// updates.getDifference; the journal guarantees that nothing with a pts is
// lost.
const (
	// PushCallTimeout bounds one call to session.
	PushCallTimeout = 2 * time.Second
	// PushRetryDelay is the pause before the only retry of a device that
	// could not take the push (FailedPrecondition).
	PushRetryDelay = 100 * time.Millisecond
	// NodePauseLimit is how long pushes wait for an unavailable node.
	NodePauseLimit = 30 * time.Second
	// PushMaxAge drops pushes older than this: the devices get one
	// updatesTooLong instead of a stream of stale pushes.
	PushMaxAge = 2 * time.Minute
	// ResyncTTL is how long a device stays marked for resync.
	ResyncTTL = 30 * time.Second
	// ResyncSweep is how often marked devices are retried without pushes.
	ResyncSweep = time.Second
	// nodeConcurrency bounds parallel calls to one node.
	nodeConcurrency = 64
)

// Outcomes of sync_push_total.
const (
	pushOK       = "ok"
	pushRetried  = "retried"
	pushDropped  = "dropped"
	pushNotFound = "not_found"
	pushStale    = "stale"
	pushResync   = "resync"
)

var (
	metricPushTotal = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "sync", Subsystem: "push", Name: "total",
		Help:   "Pushes to devices by outcome.",
		Labels: []string{"outcome"},
	})
	metricPushDuration = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "sync", Subsystem: "push", Name: "duration_ms",
		Help:    "Time to deliver a push to one device, in milliseconds.",
		Buckets: []float64{1, 5, 10, 50, 100, 500, 1000, 2000, 5000, 30000},
		Labels:  []string{},
	})
	metricResyncDevices = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "sync", Subsystem: "push", Name: "resync_devices",
		Help:   "Devices marked for resync.",
		Labels: []string{},
	})
	metricNodePaused = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "sync", Subsystem: "node", Name: "paused",
		Help:   "1 while a session node is unavailable.",
		Labels: []string{"node"},
	})
)

// sessionNode is one session node: the calls a pusher needs.
type sessionNode interface {
	pushUpdates(ctx context.Context, in *session.TLSessionPushUpdatesData) error
	pushSessionUpdates(ctx context.Context, in *session.TLSessionPushSessionUpdatesData) error
	pushRpcResult(ctx context.Context, in *session.TLSessionPushRpcResultData) error
	// waitReady blocks until the connection is ready or ctx ends.
	waitReady(ctx context.Context) bool
	// resetBackoff makes the next reconnect attempt immediate.
	resetBackoff()
	close()
}

// device identifies a device on a node.
type device struct {
	node  string
	keyId int64
}

type resyncMark struct {
	since   time.Time
	next    time.Time
	backoff time.Duration
}

// pushCall is one push to one device.
type pushCall struct {
	keyId int64
	// recoverable: a lost push is found by getDifference, so a device
	// that missed it is marked for updatesTooLong.
	recoverable bool
	call        func(ctx context.Context, n sessionNode) error
}

type nodeState struct {
	node        sessionNode
	sem         chan struct{}
	mu          sync.Mutex
	pausedSince time.Time
}

// Pusher delivers pushes to session nodes.
type Pusher struct {
	newNode func(addr string) (sessionNode, error)
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) bool

	mu     sync.RWMutex
	nodes  map[string]*nodeState
	marks  map[device]*resyncMark
	stop   chan struct{}
	closed sync.WaitGroup
}

func newPusher(newNode func(addr string) (sessionNode, error)) *Pusher {
	return &Pusher{
		newNode: newNode,
		now:     time.Now,
		sleep:   sleepCtx,
		nodes:   map[string]*nodeState{},
		marks:   map[device]*resyncMark{},
		stop:    make(chan struct{}),
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// SetNodes applies the session nodes registered in etcd. An empty list is
// applied too: all pushers close.
func (p *Pusher) SetNodes(addrs []string) {
	want := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		want[a] = true
	}
	var closing []sessionNode
	p.mu.Lock()
	for a := range want {
		if st, ok := p.nodes[a]; ok {
			// Seen again in etcd: the node may be back after a restart.
			st.node.resetBackoff()
			continue
		}
		n, err := p.newNode(a)
		if err != nil {
			logx.Errorf("sync: session node %s: %v", a, err)
			continue
		}
		p.nodes[a] = &nodeState{node: n, sem: make(chan struct{}, nodeConcurrency)}
		metricNodePaused.Set(0, a)
	}
	for a, st := range p.nodes {
		if want[a] {
			continue
		}
		closing = append(closing, st.node)
		delete(p.nodes, a)
		metricNodePaused.Set(0, a)
		// Devices of a gone node reconnect elsewhere and get
		// new_session_created.
		for d := range p.marks {
			if d.node == a {
				delete(p.marks, d)
			}
		}
	}
	metricResyncDevices.Set(float64(len(p.marks)))
	p.mu.Unlock()
	for _, n := range closing {
		n.close()
	}
}

// Start runs the resync sweep.
func (p *Pusher) Start() {
	p.closed.Add(1)
	go func() {
		defer p.closed.Done()
		t := time.NewTicker(ResyncSweep)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				p.sweep()
			}
		}
	}()
}

// Close stops the sweep and closes all nodes.
func (p *Pusher) Close() {
	close(p.stop)
	p.closed.Wait()
	p.SetNodes(nil)
}

// Batch collects the pushes of one Sync-T record; Wait returns when all of
// them are done.
type Batch struct {
	wg       sync.WaitGroup
	recorded time.Time
}

type batchKey struct{}

// WithBatch returns a context whose pushes join b. recorded is the time the
// record was written (its age decides PushMaxAge).
func WithBatch(ctx context.Context, recorded time.Time) (context.Context, *Batch) {
	b := &Batch{recorded: recorded}
	return context.WithValue(ctx, batchKey{}, b), b
}

// Wait blocks until every push of the batch is done.
func (b *Batch) Wait() { b.wg.Wait() }

// deliver runs one push to one device: in the background of the batch in
// ctx, or synchronously without a batch.
func (p *Pusher) deliver(ctx context.Context, node string, pc pushCall) {
	b, _ := ctx.Value(batchKey{}).(*Batch)
	if b == nil {
		p.deliverNow(ctx, node, pc, time.Time{})
		return
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		p.deliverNow(ctx, node, pc, b.recorded)
	}()
}

func (p *Pusher) deliverNow(ctx context.Context, node string, pc pushCall, recorded time.Time) {
	ctx = context.WithoutCancel(ctx)
	dev := device{node: node, keyId: pc.keyId}
	if !recorded.IsZero() && p.now().Sub(recorded) > PushMaxAge {
		metricPushTotal.Inc(pushStale)
		if pc.recoverable {
			p.mark(dev)
		}
		return
	}

	p.mu.RLock()
	st := p.nodes[node]
	p.mu.RUnlock()
	if st == nil {
		// Not in etcd yet (a restart): the node's pusher takes the mark
		// when it appears.
		metricPushTotal.Inc(pushDropped)
		p.mark(dev)
		return
	}

	start := p.now()
	defer func() { metricPushDuration.Observe(int64(p.now().Sub(start) / time.Millisecond)) }()

	if p.marked(dev) && !p.sendTooLong(ctx, st, dev) {
		// Still unreachable: this push would only open a gap.
		metricPushTotal.Inc(pushDropped)
		return
	}
	switch p.call(ctx, st, pc.call) {
	case pushOK:
		metricPushTotal.Inc(pushOK)
	case pushNotFound:
		metricPushTotal.Inc(pushNotFound)
	default:
		metricPushTotal.Inc(pushDropped)
		p.mark(dev)
	}
}

// call makes one push with the retry rules of ADR 0012, p. 7, and returns
// pushOK, pushNotFound or pushDropped.
func (p *Pusher) call(ctx context.Context, st *nodeState, f func(ctx context.Context, n sessionNode) error) string {
	retried := false
	for {
		if !p.waitNode(ctx, st) {
			return pushDropped
		}
		st.sem <- struct{}{}
		cctx, cancel := context.WithTimeout(ctx, PushCallTimeout)
		err := f(cctx, st.node)
		cancel()
		<-st.sem
		switch status.Code(err) {
		case codes.OK:
			p.resume(st)
			return pushOK
		case codes.NotFound:
			p.resume(st)
			return pushNotFound
		case codes.FailedPrecondition:
			if retried {
				return pushDropped
			}
			retried = true
			metricPushTotal.Inc(pushRetried)
			if !p.sleep(ctx, PushRetryDelay) {
				return pushDropped
			}
		case codes.Unavailable:
			p.pause(st)
		default:
			// DeadlineExceeded on a ready connection: one device hangs.
			return pushDropped
		}
	}
}

// waitNode waits for a paused node, but not longer than NodePauseLimit since
// the pause began. Past the limit it does not wait at all, but a node whose
// connection is ready again is still tried: a successful push ends the pause.
func (p *Pusher) waitNode(ctx context.Context, st *nodeState) bool {
	st.mu.Lock()
	since := st.pausedSince
	st.mu.Unlock()
	if since.IsZero() {
		return true
	}
	left := max(NodePauseLimit-p.now().Sub(since), 0)
	wctx, cancel := context.WithTimeout(ctx, left)
	defer cancel()
	return st.node.waitReady(wctx)
}

func (p *Pusher) pause(st *nodeState) {
	st.mu.Lock()
	if st.pausedSince.IsZero() {
		st.pausedSince = p.now()
	}
	st.mu.Unlock()
	p.setPausedMetric(st, 1)
}

func (p *Pusher) resume(st *nodeState) {
	st.mu.Lock()
	was := !st.pausedSince.IsZero()
	st.pausedSince = time.Time{}
	st.mu.Unlock()
	if was {
		p.setPausedMetric(st, 0)
	}
}

func (p *Pusher) setPausedMetric(st *nodeState, v float64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for a, s := range p.nodes {
		if s == st {
			metricNodePaused.Set(v, a)
		}
	}
}

func (p *Pusher) mark(dev device) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.marks[dev]; !ok {
		now := p.now()
		p.marks[dev] = &resyncMark{since: now, next: now, backoff: ResyncSweep}
		metricPushTotal.Inc(pushResync)
	}
	metricResyncDevices.Set(float64(len(p.marks)))
}

func (p *Pusher) marked(dev device) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.marks[dev]
	return ok
}

// sendTooLong tells a marked device to call updates.getDifference and
// clears the mark on success.
func (p *Pusher) sendTooLong(ctx context.Context, st *nodeState, dev device) bool {
	res := p.call(ctx, st, func(ctx context.Context, n sessionNode) error {
		return n.pushUpdates(ctx, &session.TLSessionPushUpdatesData{
			PermAuthKeyId: dev.keyId,
			Updates:       mtproto.MakeTLUpdatesTooLong(nil).To_Updates(),
		})
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.marks[dev]
	if m == nil {
		return true
	}
	if res != pushDropped {
		// Delivered, or the key is gone: nothing left to catch up with.
		delete(p.marks, dev)
		metricResyncDevices.Set(float64(len(p.marks)))
		return true
	}
	m.backoff *= 2
	if m.backoff > ResyncTTL {
		m.backoff = ResyncTTL
	}
	m.next = p.now().Add(m.backoff)
	return false
}

// sweep retries marked devices that get no pushes and drops marks older
// than ResyncTTL: such a device is unreachable and gets new_session_created
// when it reconnects.
func (p *Pusher) sweep() {
	now := p.now()
	type due struct {
		dev device
		st  *nodeState
	}
	var todo []due
	p.mu.Lock()
	for dev, m := range p.marks {
		if now.Sub(m.since) > ResyncTTL {
			delete(p.marks, dev)
			continue
		}
		if st := p.nodes[dev.node]; st != nil && !now.Before(m.next) {
			todo = append(todo, due{dev, st})
		}
	}
	metricResyncDevices.Set(float64(len(p.marks)))
	p.mu.Unlock()

	var wg sync.WaitGroup
	for _, d := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.sendTooLong(context.Background(), d.st, d.dev)
		}()
	}
	wg.Wait()
}

// recoverable reports whether updates carry a pts or a seq: such a push can
// be recovered with getDifference (an update with seq from the seq journal,
// ADR 0020), others (typing, calls) are best effort.
func recoverable(u *mtproto.Updates) bool {
	if u == nil {
		return false
	}
	if u.GetPts() > 0 || u.GetSeq() > 0 {
		return true
	}
	if up := u.GetUpdate(); up != nil && up.GetPts_INT32() > 0 {
		return true
	}
	for _, up := range u.GetUpdates() {
		if up.GetPts_INT32() > 0 {
			return true
		}
	}
	return false
}

// PushUpdates pushes updates to a device (all its sessions).
func (d *Dao) PushUpdates(ctx context.Context, node string, in *session.TLSessionPushUpdatesData) {
	d.pusher.deliver(ctx, node, pushCall{
		keyId:       in.PermAuthKeyId,
		recoverable: recoverable(in.Updates),
		call: func(ctx context.Context, n sessionNode) error {
			return n.pushUpdates(ctx, in)
		},
	})
}

// PushSessionUpdates pushes updates to one session of a device.
func (d *Dao) PushSessionUpdates(ctx context.Context, node string, in *session.TLSessionPushSessionUpdatesData) {
	d.pusher.deliver(ctx, node, pushCall{
		keyId:       in.PermAuthKeyId,
		recoverable: recoverable(in.Updates),
		call: func(ctx context.Context, n sessionNode) error {
			return n.pushSessionUpdates(ctx, in)
		},
	})
}

// PushRpcResult pushes an rpc_result to the session that sent the request.
func (d *Dao) PushRpcResult(ctx context.Context, node string, in *session.TLSessionPushRpcResultData) {
	d.pusher.deliver(ctx, node, pushCall{
		keyId: in.PermAuthKeyId,
		call: func(ctx context.Context, n sessionNode) error {
			return n.pushRpcResult(ctx, in)
		},
	})
}
