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

//go:build integration

package cdn

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMySQLTwoPreparers runs two preparers (two processes) over one queue:
// each file is prepared once, and a preparation whose preparer stopped is
// taken over once its lease runs out.
func TestMySQLTwoPreparers(t *testing.T) {
	ctx := context.Background()
	store := itStore()
	set := testSettings(t)
	target := newFakeTarget(suiteTarget(), false)
	reader := newMemReader()

	var keys []FileKey
	for range 4 {
		key := FileKey{Kind: KindDocument, Id: suiteId()}
		data := refFile()
		reader.add(key, data)
		f := ensure(t, store, key, int64(len(data)))
		if err := store.Enqueue(ctx, f.RowId, target.ID(), 0, time.Now().UnixMilli()); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}

	var ready atomic.Int32
	onReady := func(FileKey) { ready.Add(1) }
	a := NewPreparer(store, target, reader, set, onReady)
	b := NewPreparer(store, target, reader, set, onReady)
	var wg sync.WaitGroup
	for _, p := range []*Preparer{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p.Next(ctx) {
			}
		}()
	}
	wg.Wait()
	// A Claim that lost every race returns none though some are left.
	for a.Next(ctx) {
	}
	if n := ready.Load(); n != int32(len(keys)) {
		t.Errorf("%d preparations made ready, want %d (one each)", n, len(keys))
	}
	for _, key := range keys {
		f, err := store.Get(ctx, key)
		if err != nil || f == nil || f.Hashes == nil || !f.HasKey() {
			t.Fatalf("file %d: %v, %v", key.Id, f, err)
		}
		r, err := store.Replica(ctx, f.RowId, target.ID())
		if err != nil || r == nil || r.State != StateReady || r.Attempts != 1 {
			t.Errorf("file %d: %+v, %v; want ready at the first attempt", key.Id, r, err)
		}
	}

	// A preparer that stopped mid-way: its claim lapses and another takes
	// the preparation.
	key := FileKey{Kind: KindDocument, Id: suiteId()}
	reader.add(key, refFile())
	f := ensure(t, store, key, refSize)
	if err := store.Enqueue(ctx, f.RowId, target.ID(), 0, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if c, err := store.Claim(ctx, target.ID(), time.Second, time.Now().UnixMilli()); err != nil || c == nil {
		t.Fatalf("Claim: %v, %v", c, err)
	}
	if a.Next(ctx) {
		t.Fatal("a held preparation was claimed again")
	}
	time.Sleep(1100 * time.Millisecond)
	if !a.Next(ctx) {
		t.Fatal("the lapsed preparation was not taken over")
	}
	r, err := store.Replica(ctx, f.RowId, target.ID())
	if err != nil || r == nil || r.State != StateReady || r.Attempts != 2 {
		t.Errorf("%+v, %v; want ready at the second attempt", r, err)
	}
}

// TestMySQLUsage counts the traffic of a delivery from two processes into
// cdn_delivery_usage.
func TestMySQLUsage(t *testing.T) {
	ctx := context.Background()
	store := itStore()
	dc := suiteDelivery()
	deliveries := []Delivery{{ID: dc, MonthlyBytes: 10_000}}
	a, b := NewUsage(store, deliveries), NewUsage(store, deliveries)
	a.CountOrigin(dc, 4000)
	a.NoteRedirect(dc, 1, 1, 3000)
	b.CountOrigin(dc, 2999)
	for _, u := range []*Usage{a, b, a} {
		if err := u.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	month := UsageMonth(time.Now())
	got, err := store.Usage(ctx, dc, month)
	if err != nil || got.OriginBytes != 6999 || got.EdgeBytesEst != 3000 {
		t.Fatalf("%+v, %v; want 6999 and 3000", got, err)
	}
	if a.OverBudget(dc) {
		t.Error("over at 9999 of 10000")
	}
	b.CountOrigin(dc, 1)
	if !b.OverBudget(dc) {
		t.Error("not over at 10000 of 10000")
	}
}
