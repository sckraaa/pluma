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

package cdn

import (
	"context"
	"errors"
	"testing"
	"time"
)

// usageEnv is a Usage of one delivery with a budget of 1000 bytes over a
// MemStore.
type usageEnv struct {
	t     *testing.T
	ctx   context.Context
	store *MemStore
	clock *clock
	usage *Usage
}

func newUsageEnv(t *testing.T, store *MemStore) *usageEnv {
	t.Helper()
	e := &usageEnv{t: t, ctx: context.Background(), store: store, clock: newClock()}
	e.usage = NewUsage(store, []Delivery{{ID: testDc, MonthlyBytes: 1000}})
	e.usage.now = e.clock.now
	return e
}

func (e *usageEnv) stored(month string) DeliveryUsage {
	e.t.Helper()
	u, err := e.store.Usage(e.ctx, testDc, month)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func TestUsageEdgeOncePerDay(t *testing.T) {
	e := newUsageEnv(t, NewMemStore())
	u := e.usage
	u.NoteRedirect(testDc, 1, 10, 300)
	u.NoteRedirect(testDc, 1, 10, 300) // the same key and file: the same download
	u.NoteRedirect(testDc, 2, 10, 300) // another key
	u.NoteRedirect(testDc, 1, 11, 100) // another file
	u.NoteRedirect(202, 1, 10, 50)     // another delivery
	if o, edge := u.total(testDc, UsageMonth(e.clock.now())); o != 0 || edge != 700 {
		t.Errorf("origin %d, edge %d; want 0, 700", o, edge)
	}
	e.clock.add(edgeSeenFor - time.Second)
	u.NoteRedirect(testDc, 1, 10, 300)
	if _, edge := u.total(testDc, UsageMonth(e.clock.now())); edge != 700 {
		t.Errorf("counted again within a day: %d", edge)
	}
	e.clock.add(time.Second)
	u.NoteRedirect(testDc, 1, 10, 300)
	if _, edge := u.total(testDc, UsageMonth(e.clock.now())); edge != 1000 {
		t.Errorf("not counted again after a day: %d", edge)
	}
}

func TestUsageBudget(t *testing.T) {
	e := newUsageEnv(t, NewMemStore())
	u := e.usage
	u.CountOrigin(testDc, 600)
	if u.OverBudget(testDc) {
		t.Error("over the budget at 600 of 1000")
	}
	if err := u.Flush(e.ctx); err != nil {
		t.Fatal(err)
	}
	month := UsageMonth(e.clock.now())
	if got := e.stored(month); got.OriginBytes != 600 || got.EdgeBytesEst != 0 {
		t.Errorf("stored %+v", got)
	}
	u.NoteRedirect(testDc, 1, 1, 400) // 600 stored + 400 pending
	if !u.OverBudget(testDc) {
		t.Error("not over the budget at 1000 of 1000")
	}
	if err := u.Flush(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stored(month); got.OriginBytes != 600 || got.EdgeBytesEst != 400 {
		t.Errorf("stored %+v", got)
	}
	if !u.OverBudget(testDc) {
		t.Error("not over the budget after the flush")
	}
	if u.OverBudget(999) {
		t.Error("a delivery the budget does not know is over it")
	}
}

// TestUsageProcesses checks that two processes count into one row and
// each sees the other's traffic after its flush.
func TestUsageProcesses(t *testing.T) {
	store := NewMemStore()
	a, b := newUsageEnv(t, store), newUsageEnv(t, store)
	a.usage.CountOrigin(testDc, 500)
	b.usage.CountOrigin(testDc, 499)
	if err := a.usage.Flush(a.ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.usage.Flush(b.ctx); err != nil {
		t.Fatal(err)
	}
	if b.usage.OverBudget(testDc) {
		t.Error("over at 999")
	}
	b.usage.CountOrigin(testDc, 1)
	if !b.usage.OverBudget(testDc) {
		t.Error("b does not see the traffic of a")
	}
	if err := b.usage.Flush(b.ctx); err != nil {
		t.Fatal(err)
	}
	if a.usage.OverBudget(testDc) {
		t.Error("a sees the traffic of b before its flush")
	}
	if err := a.usage.Flush(a.ctx); err != nil {
		t.Fatal(err)
	}
	if !a.usage.OverBudget(testDc) {
		t.Error("a does not see the traffic of b after its flush")
	}
}

func TestUsageMonthChange(t *testing.T) {
	e := newUsageEnv(t, NewMemStore())
	u := e.usage
	e.clock.add(time.Date(2026, 11, 30, 23, 59, 0, 0, time.UTC).Sub(e.clock.now()))
	u.CountOrigin(testDc, 900)
	u.NoteRedirect(testDc, 1, 1, 200)
	if !u.OverBudget(testDc) {
		t.Fatal("not over at 1100 in November")
	}
	e.clock.add(2 * time.Minute) // 1 December, UTC
	if u.OverBudget(testDc) {
		t.Error("the budget of November held in December")
	}
	u.CountOrigin(testDc, 10)
	if err := u.Flush(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stored("2026-11"); got.OriginBytes != 900 || got.EdgeBytesEst != 200 {
		t.Errorf("November: %+v", got)
	}
	if got := e.stored("2026-12"); got.OriginBytes != 10 {
		t.Errorf("December: %+v", got)
	}
	if u.OverBudget(testDc) {
		t.Error("over in December at 10")
	}
}

func TestUsageFlushFailureKeepsCounts(t *testing.T) {
	e := newUsageEnv(t, NewMemStore())
	u := e.usage
	u.CountOrigin(testDc, 300)
	e.store.SetErr(errors.New("mysql is down"))
	if err := u.Flush(e.ctx); err == nil {
		t.Fatal("no error with the store down")
	}
	u.CountOrigin(testDc, 200)
	if o, _ := u.total(testDc, UsageMonth(e.clock.now())); o != 500 {
		t.Errorf("%d bytes counted with the store down, want 500", o)
	}
	e.store.SetErr(nil)
	if err := u.Flush(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stored(UsageMonth(e.clock.now())); got.OriginBytes != 500 {
		t.Errorf("stored %d bytes, want 500 once", got.OriginBytes)
	}
	if err := u.Flush(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stored(UsageMonth(e.clock.now())); got.OriginBytes != 500 {
		t.Errorf("stored %d bytes after another flush, want 500", got.OriginBytes)
	}
}

func TestUsageRunFlushesAtStop(t *testing.T) {
	e := newUsageEnv(t, NewMemStore())
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.usage.Run(ctx)
	}()
	e.usage.CountOrigin(testDc, 123)
	cancel()
	<-done
	if got := e.stored(UsageMonth(e.clock.now())); got.OriginBytes != 123 {
		t.Errorf("stored %d bytes at the stop, want 123", got.OriginBytes)
	}
}

func TestEdgeSeenBound(t *testing.T) {
	s := newEdgeSeen(2, time.Hour)
	now := time.Now()
	for i := range int64(3) {
		if !s.first(edgePair{dc: 1, authKeyId: i, fileRowId: 1}, now) {
			t.Fatalf("pair %d seen", i)
		}
	}
	// The least recent was dropped and counts again; the others do not.
	if !s.first(edgePair{dc: 1, authKeyId: 0, fileRowId: 1}, now) {
		t.Error("the dropped pair is still remembered")
	}
	if s.first(edgePair{dc: 1, authKeyId: 2, fileRowId: 1}, now) {
		t.Error("a remembered pair counted again")
	}
	if len(s.byKey) != 2 || s.order.Len() != 2 {
		t.Errorf("%d pairs kept, want 2", len(s.byKey))
	}
}
