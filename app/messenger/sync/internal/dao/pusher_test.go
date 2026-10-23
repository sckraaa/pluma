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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/session"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeNode answers pushes with scripted codes and records what it got.
type fakeNode struct {
	mu      sync.Mutex
	answers []codes.Code // consumed in order; OK when empty
	ready   bool
	got     []string // "updates:<key>", "toolong:<key>", "session:<key>", "rpc:<key>"
	closed  bool
	resets  int
}

func (n *fakeNode) answer() error {
	if len(n.answers) == 0 {
		return nil
	}
	c := n.answers[0]
	n.answers = n.answers[1:]
	if c == codes.OK {
		return nil
	}
	return status.Error(c, c.String())
}

func (n *fakeNode) pushUpdates(_ context.Context, in *session.TLSessionPushUpdatesData) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	err := n.answer()
	if err == nil {
		kind := "updates"
		if in.Updates.GetPredicateName() == mtproto.Predicate_updatesTooLong {
			kind = "toolong"
		}
		n.got = append(n.got, kind+":"+itoa(in.PermAuthKeyId))
	}
	return err
}

func (n *fakeNode) pushSessionUpdates(_ context.Context, in *session.TLSessionPushSessionUpdatesData) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	err := n.answer()
	if err == nil {
		n.got = append(n.got, "session:"+itoa(in.PermAuthKeyId))
	}
	return err
}

func (n *fakeNode) pushRpcResult(_ context.Context, in *session.TLSessionPushRpcResultData) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	err := n.answer()
	if err == nil {
		n.got = append(n.got, "rpc:"+itoa(in.PermAuthKeyId))
	}
	return err
}

func (n *fakeNode) waitReady(ctx context.Context) bool {
	for {
		n.mu.Lock()
		r := n.ready
		n.mu.Unlock()
		if r {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Millisecond):
		}
	}
}

func (n *fakeNode) resetBackoff() { n.mu.Lock(); n.resets++; n.mu.Unlock() }
func (n *fakeNode) close()        { n.mu.Lock(); n.closed = true; n.mu.Unlock() }

func (n *fakeNode) calls() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.got...)
}

func itoa(v int64) string { return time.Duration(v).String() }

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestPusher(t *testing.T, nodes map[string]*fakeNode) (*Pusher, *clock) {
	t.Helper()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	p := newPusher(func(addr string) (sessionNode, error) { return nodes[addr], nil })
	p.now = c.now
	p.sleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	addrs := make([]string, 0, len(nodes))
	for a := range nodes {
		addrs = append(addrs, a)
	}
	p.SetNodes(addrs)
	return p, c
}

func ptsUpdates() *mtproto.Updates {
	return mtproto.MakeTLUpdateShort(&mtproto.Updates{
		Update: mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Pts_INT32: 7, PtsCount: 1}).To_Update(),
	}).To_Updates()
}

func push(ctx context.Context, d *Dao, node string, key int64, u *mtproto.Updates) {
	d.PushUpdates(ctx, node, &session.TLSessionPushUpdatesData{PermAuthKeyId: key, Updates: u})
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("calls %v, want %v", got, want)
		}
	}
}

func TestPushDeliveredAndBatchWaits(t *testing.T) {
	n := &fakeNode{ready: true}
	p, c := newTestPusher(t, map[string]*fakeNode{"s1": n})
	d := &Dao{pusher: p}

	ctx, b := WithBatch(context.Background(), c.now())
	push(ctx, d, "s1", 1, ptsUpdates())
	push(ctx, d, "s1", 2, ptsUpdates())
	b.Wait()

	got := n.calls()
	if len(got) != 2 || p.marked(device{"s1", 1}) || p.marked(device{"s1", 2}) {
		t.Fatalf("calls %v, marks %v", got, p.marks)
	}
}

func TestPushNotFoundDropsWithoutResync(t *testing.T) {
	n := &fakeNode{ready: true, answers: []codes.Code{codes.NotFound}}
	p, _ := newTestPusher(t, map[string]*fakeNode{"s1": n})
	push(context.Background(), &Dao{pusher: p}, "s1", 1, ptsUpdates())
	if p.marked(device{"s1", 1}) {
		t.Fatal("unknown key marked for resync")
	}
}

func TestPushFailedPreconditionRetriesOnce(t *testing.T) {
	n := &fakeNode{ready: true, answers: []codes.Code{codes.FailedPrecondition, codes.OK}}
	p, _ := newTestPusher(t, map[string]*fakeNode{"s1": n})
	d := &Dao{pusher: p}
	push(context.Background(), d, "s1", 1, ptsUpdates())
	eq(t, n.calls(), "updates:1ns")
	if p.marked(device{"s1", 1}) {
		t.Fatal("marked after a successful retry")
	}
}

func TestPushDroppedDeviceGetsTooLongBeforeNextPush(t *testing.T) {
	n := &fakeNode{ready: true, answers: []codes.Code{codes.FailedPrecondition, codes.FailedPrecondition}}
	p, _ := newTestPusher(t, map[string]*fakeNode{"s1": n})
	d := &Dao{pusher: p}

	push(context.Background(), d, "s1", 1, ptsUpdates())
	if !p.marked(device{"s1", 1}) {
		t.Fatal("dropped push did not mark the device")
	}
	push(context.Background(), d, "s1", 1, ptsUpdates())
	eq(t, n.calls(), "toolong:1ns", "updates:1ns")
	if p.marked(device{"s1", 1}) {
		t.Fatal("mark kept after updatesTooLong was delivered")
	}
}

func TestPushHungDeviceIsDroppedAndMarked(t *testing.T) {
	n := &fakeNode{ready: true, answers: []codes.Code{codes.DeadlineExceeded}}
	p, _ := newTestPusher(t, map[string]*fakeNode{"s1": n})
	push(context.Background(), &Dao{pusher: p}, "s1", 1, ptsUpdates())
	if !p.marked(device{"s1", 1}) {
		t.Fatal("timed out push did not mark the device")
	}
}

func TestPushWaitsForUnavailableNode(t *testing.T) {
	n := &fakeNode{answers: []codes.Code{codes.Unavailable}}
	p, _ := newTestPusher(t, map[string]*fakeNode{"s1": n})
	done := make(chan struct{})
	go func() {
		push(context.Background(), &Dao{pusher: p}, "s1", 1, ptsUpdates())
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	n.mu.Lock()
	n.ready = true
	n.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("push did not resume when the node came back")
	}
	eq(t, n.calls(), "updates:1ns")
	if st := p.nodes["s1"]; !st.pausedSince.IsZero() {
		t.Fatal("node still paused after a successful push")
	}
}

func TestPushGivesUpOnNodePausedTooLong(t *testing.T) {
	n := &fakeNode{answers: []codes.Code{codes.Unavailable}}
	p, c := newTestPusher(t, map[string]*fakeNode{"s1": n})
	d := &Dao{pusher: p}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The node has been paused for longer than the limit: the push is
	// dropped at once instead of waiting.
	p.pause(p.nodes["s1"])
	c.add(NodePauseLimit + time.Second)
	push(ctx, d, "s1", 1, ptsUpdates())
	if !p.marked(device{"s1", 1}) {
		t.Fatal("push to a node paused too long was not marked")
	}
}

func TestStalePush(t *testing.T) {
	n := &fakeNode{ready: true}
	p, c := newTestPusher(t, map[string]*fakeNode{"s1": n})
	d := &Dao{pusher: p}

	ctx, b := WithBatch(context.Background(), c.now().Add(-PushMaxAge-time.Second))
	push(ctx, d, "s1", 1, ptsUpdates())
	push(ctx, d, "s1", 2, mtproto.MakeTLUpdateShort(&mtproto.Updates{
		Update: mtproto.MakeTLUpdateUserTyping(nil).To_Update(),
	}).To_Updates())
	b.Wait()

	eq(t, n.calls())
	if !p.marked(device{"s1", 1}) {
		t.Fatal("stale push with pts did not mark the device")
	}
	if p.marked(device{"s1", 2}) {
		t.Fatal("stale push without pts marked the device")
	}
}

func TestUnknownNodeMarksAndSweepCatchesUpWhenItAppears(t *testing.T) {
	nodes := map[string]*fakeNode{"s1": {ready: true}}
	p, c := newTestPusher(t, nodes)
	push(context.Background(), &Dao{pusher: p}, "s2", 1, ptsUpdates())
	if !p.marked(device{"s2", 1}) {
		t.Fatal("push to an unknown node did not mark the device")
	}

	nodes["s2"] = &fakeNode{ready: true}
	p.SetNodes([]string{"s1", "s2"})
	c.add(ResyncSweep)
	p.sweep()
	eq(t, nodes["s2"].calls(), "toolong:1ns")
	if p.marked(device{"s2", 1}) {
		t.Fatal("mark kept after the sweep delivered updatesTooLong")
	}
}

func TestSweepBacksOffAndExpires(t *testing.T) {
	n := &fakeNode{ready: true, answers: []codes.Code{codes.DeadlineExceeded}}
	p, c := newTestPusher(t, map[string]*fakeNode{"s1": n})
	dev := device{"s1", 1}
	p.mark(dev)

	p.sweep() // fails: next try after a backoff
	if !p.marked(dev) {
		t.Fatal("mark lost after a failed sweep")
	}
	p.sweep() // too early: no call
	eq(t, n.calls())

	c.add(ResyncTTL + time.Second)
	p.sweep()
	if p.marked(dev) {
		t.Fatal("mark older than ResyncTTL kept")
	}
}

func TestSetNodesClosesRemovedAndAppliesEmptyList(t *testing.T) {
	nodes := map[string]*fakeNode{"s1": {ready: true}, "s2": {ready: true}}
	p, _ := newTestPusher(t, nodes)
	p.mark(device{"s2", 9})

	p.SetNodes([]string{"s1"})
	if !nodes["s2"].closed || p.marked(device{"s2", 9}) {
		t.Fatal("removed node not closed or its marks kept")
	}
	p.SetNodes([]string{"s1"})
	// Every etcd update that still lists a node resets its reconnect
	// backoff: a restarted node is reached at once.
	if nodes["s1"].resets != 2 {
		t.Fatalf("node listed again twice: %d backoff resets, want 2", nodes["s1"].resets)
	}
	p.SetNodes(nil)
	if !nodes["s1"].closed || len(p.nodes) != 0 {
		t.Fatal("empty node list not applied")
	}
}

func TestPushOrderPerDeviceAcrossRecords(t *testing.T) {
	n := &fakeNode{ready: true}
	p, c := newTestPusher(t, map[string]*fakeNode{"s1": n})
	d := &Dao{pusher: p}
	// Records of one user are consumed one after another, each waiting for
	// its batch: pushes reach the device in record order.
	for i := 0; i < 50; i++ {
		ctx, b := WithBatch(context.Background(), c.now())
		if i%2 == 0 {
			push(ctx, d, "s1", 1, ptsUpdates())
		} else {
			d.PushRpcResult(ctx, "s1", &session.TLSessionPushRpcResultData{PermAuthKeyId: 1})
		}
		b.Wait()
	}
	got := n.calls()
	if len(got) != 50 {
		t.Fatalf("%d calls, want 50", len(got))
	}
	for i, g := range got {
		want := "updates:1ns"
		if i%2 == 1 {
			want = "rpc:1ns"
		}
		if g != want {
			t.Fatalf("call %d = %s, want %s", i, g, want)
		}
	}
}

func TestHasPts(t *testing.T) {
	if !hasPts(ptsUpdates()) {
		t.Fatal("updateShort with pts")
	}
	if hasPts(mtproto.MakeTLUpdatesTooLong(nil).To_Updates()) || hasPts(nil) {
		t.Fatal("no pts")
	}
	many := mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{
		mtproto.MakeTLUpdateUserTyping(nil).To_Update(),
		mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Pts_INT32: 3, PtsCount: 1}).To_Update(),
	}}).To_Updates()
	if !hasPts(many) {
		t.Fatal("updates with one pts update")
	}
}
