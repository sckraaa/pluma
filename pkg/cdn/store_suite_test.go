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
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The behaviour every Store has: run on the MemStore by the unit tests and
// on MySQL by the integration tests, so the two cannot drift apart.

// suiteSeq makes file ids, targets and deliveries unique across tests and
// runs on one database (go test -count=N).
var suiteSeq atomic.Int64

func suiteId() int64 { return time.Now().UnixMilli()%1_000_000_000*1000 + suiteSeq.Add(1) }

// suiteTarget is a target of its own for a test, so the queues of tests do
// not mix; suiteDelivery likewise a delivery.
func suiteTarget() int32 { return int32(1000 + suiteId()%1_000_000_000) }

func suiteDelivery() int32 { return suiteTarget() }

func mustSecrets(t *testing.T) Secrets {
	t.Helper()
	s, err := NewSecrets()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ensure(t *testing.T, s Store, key FileKey, size int64) *File {
	t.Helper()
	f, err := s.Ensure(context.Background(), &File{Key: key, Size: size, Eligible: Eligible(size, DefaultMinFileSize), CreatedAt: 1})
	if err != nil || f == nil {
		t.Fatalf("Ensure: %v, %v", f, err)
	}
	return f
}

func runStoreSuite(t *testing.T, s Store) {
	t.Run("files", func(t *testing.T) { suiteFiles(t, s) })
	t.Run("queue", func(t *testing.T) { suiteQueue(t, s) })
	t.Run("fencing", func(t *testing.T) { suiteFencing(t, s) })
	t.Run("failures", func(t *testing.T) { suiteFailures(t, s) })
	t.Run("priority first", func(t *testing.T) { suitePriorityFirst(t, s) })
	t.Run("deliveries", func(t *testing.T) { suiteDeliveries(t, s) })
	t.Run("concurrent", func(t *testing.T) { suiteConcurrent(t, s) })
}

func suiteFiles(t *testing.T, s Store) {
	ctx := context.Background()
	key := FileKey{Kind: KindPhoto, Id: suiteId(), Thumb: "y"}
	if f, err := s.Get(ctx, key); f != nil || err != nil {
		t.Fatalf("Get before Ensure: %v, %v", f, err)
	}
	// A row made before the size is known takes it later, once.
	f := ensure(t, s, key, 0)
	if f.Key != key || f.Size != 0 || f.HasKey() || f.Hashes != nil {
		t.Fatalf("new row: %+v", f)
	}
	f = ensure(t, s, key, 3<<20)
	if f.Size != 3<<20 || !f.Eligible {
		t.Fatalf("size not taken: %+v", f)
	}
	row := f.RowId
	f = ensure(t, s, key, 100)
	if f.RowId != row || f.Size != 3<<20 || !f.Eligible {
		t.Fatalf("a known size changed: %+v", f)
	}
	small := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 900<<10)
	if small.Eligible || small.RowId == row {
		t.Fatalf("small file: %+v", small)
	}
	// The kinds do not share ids.
	if other := ensure(t, s, FileKey{Kind: KindEncrypted, Id: key.Id}, 5); other.RowId == row {
		t.Fatal("a secret chat file took the row of a photo with its id")
	}
	// Sizes of a photo are compared byte for byte: no case folding, no
	// trailing-space padding.
	for _, thumb := range []string{"Y", "y "} {
		other := ensure(t, s, FileKey{Kind: KindPhoto, Id: key.Id, Thumb: thumb}, 5)
		if other.RowId == row || other.Key.Thumb != thumb {
			t.Fatalf("size %q took the row of size %q: %+v", thumb, key.Thumb, other)
		}
	}

	sec := mustSecrets(t)
	got, err := s.SetKey(ctx, row, sec)
	if err != nil || got == nil || !bytes.Equal(got.Secrets.Token, sec.Token) || !bytes.Equal(got.Secrets.Key, sec.Key) || !bytes.Equal(got.Secrets.IV, sec.IV) {
		t.Fatalf("SetKey: %+v, %v", got, err)
	}
	// The key is set once.
	got, err = s.SetKey(ctx, row, mustSecrets(t))
	if err != nil || !bytes.Equal(got.Secrets.Token, sec.Token) || !bytes.Equal(got.Secrets.Key, sec.Key) {
		t.Fatalf("second SetKey replaced the key: %v", err)
	}
	if got, err = s.SetKey(ctx, -1, mustSecrets(t)); got != nil || err != nil {
		t.Fatalf("SetKey of no row: %v, %v", got, err)
	}
	if _, err = s.SetKey(ctx, row, Secrets{Token: sec.Token}); err == nil {
		t.Fatal("SetKey took secrets without a key")
	}

	byToken, err := s.ByToken(ctx, sec.Token)
	if err != nil || byToken == nil || byToken.RowId != row || byToken.Key != key {
		t.Fatalf("ByToken: %+v, %v", byToken, err)
	}
	for _, tok := range [][]byte{mustSecrets(t).Token, sec.Token[:31], nil} {
		if f, err := s.ByToken(ctx, tok); f != nil || err != nil {
			t.Fatalf("ByToken of %d unknown bytes: %v, %v", len(tok), f, err)
		}
	}
}

func suiteQueue(t *testing.T, s Store) {
	ctx := context.Background()
	dc := suiteTarget()
	const now = 10_000_000
	if c, err := s.Claim(ctx, dc, time.Minute, now); c != nil || err != nil {
		t.Fatalf("Claim of an empty queue: %v, %v", c, err)
	}
	a := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	b := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	for _, f := range []*File{b, a, a} { // idempotent
		if err := s.Enqueue(ctx, f.RowId, dc, 0, now); err != nil {
			t.Fatal(err)
		}
	}
	if r, err := s.Replica(ctx, a.RowId, dc); err != nil || r == nil || r.State != StateQueued || r.Attempts != 0 {
		t.Fatalf("queued copy: %+v, %v", r, err)
	}
	if counts, err := s.QueueCounts(ctx, dc); err != nil || counts[StateQueued] != 2 || len(counts) != 1 {
		t.Fatalf("QueueCounts: %v, %v", counts, err)
	}

	// By file, the lower row first.
	c, err := s.Claim(ctx, dc, time.Minute, now)
	if err != nil || c == nil || c.File.RowId != a.RowId || c.Attempts != 1 || c.LeaseUntil != now+60_000 || c.File.Size != 2<<20 {
		t.Fatalf("first claim: %+v, %v", c, err)
	}
	c2, err := s.Claim(ctx, dc, time.Minute, now)
	if err != nil || c2 == nil || c2.File.RowId != b.RowId {
		t.Fatalf("second claim: %+v, %v", c2, err)
	}
	if c3, err := s.Claim(ctx, dc, time.Minute, now); c3 != nil || err != nil {
		t.Fatalf("a held copy was claimed again: %+v, %v", c3, err)
	}

	sec := mustSecrets(t)
	if _, err = s.SetKey(ctx, a.RowId, sec); err != nil {
		t.Fatal(err)
	}
	hashes := bytes.Repeat([]byte{9}, 16*32)
	if ok, err := s.MarkReady(ctx, c, hashes, now+5); !ok || err != nil {
		t.Fatalf("MarkReady: %v, %v", ok, err)
	}
	r, err := s.Replica(ctx, a.RowId, dc)
	if err != nil || r.State != StateReady || r.ReadyAt != now+5 || r.LeaseUntil != 0 {
		t.Fatalf("ready copy: %+v, %v", r, err)
	}
	if f, err := s.Get(ctx, a.Key); err != nil || !bytes.Equal(f.Hashes, hashes) {
		t.Fatalf("hashes not stored: %v", err)
	}
	// Enqueue leaves a ready copy.
	if err = s.Enqueue(ctx, a.RowId, dc, PriorityFirst, now+6); err != nil {
		t.Fatal(err)
	}
	if r, _ = s.Replica(ctx, a.RowId, dc); r.State != StateReady || r.Priority != 0 {
		t.Fatalf("Enqueue changed a ready copy: %+v", r)
	}
	// The target lost it: absent, not queued until asked for again.
	if ok, err := s.MarkAbsent(ctx, a.RowId, dc, now+7); !ok || err != nil {
		t.Fatalf("MarkAbsent: %v, %v", ok, err)
	}
	if ok, err := s.MarkAbsent(ctx, a.RowId, dc, now+7); ok || err != nil {
		t.Fatalf("MarkAbsent of an absent copy: %v, %v", ok, err)
	}
	if ok, err := s.MarkAbsent(ctx, b.RowId, dc, now+7); ok || err != nil {
		t.Fatalf("MarkAbsent of a copy being pushed: %v, %v", ok, err)
	}
	if c3, err := s.Claim(ctx, dc, time.Minute, now+8); c3 != nil || err != nil {
		t.Fatalf("an absent copy was claimed: %+v, %v", c3, err)
	}
	if err = s.Enqueue(ctx, a.RowId, dc, 0, now+9); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Replica(ctx, a.RowId, dc)
	if r.State != StateQueued || r.Attempts != 0 {
		t.Fatalf("absent copy queued: %+v", r)
	}
	if counts, err := s.QueueCounts(ctx, dc); err != nil || counts[StateQueued] != 1 || counts[StatePushing] != 1 {
		t.Fatalf("QueueCounts: %v, %v", counts, err)
	}
}

func suiteFencing(t *testing.T, s Store) {
	ctx := context.Background()
	dc := suiteTarget()
	const now = 20_000_000
	f := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	if err := s.Enqueue(ctx, f.RowId, dc, 0, now); err != nil {
		t.Fatal(err)
	}
	old, err := s.Claim(ctx, dc, time.Minute, now)
	if err != nil || old == nil {
		t.Fatalf("Claim: %v, %v", old, err)
	}
	// Extended within the lease: still held, and the lease moves.
	if ok, err := s.ExtendLease(ctx, old, time.Minute, now+1000); !ok || err != nil || old.LeaseUntil != now+61_000 {
		t.Fatalf("ExtendLease: %v, %v, lease %d", ok, err, old.LeaseUntil)
	}
	// Extended in the same millisecond: still moves.
	before := old.LeaseUntil
	if ok, err := s.ExtendLease(ctx, old, time.Minute, now+1000); !ok || err != nil || old.LeaseUntil <= before {
		t.Fatalf("ExtendLease in the same millisecond: %v, %v", ok, err)
	}
	// Not before the lease expires.
	if c, err := s.Claim(ctx, dc, time.Minute, old.LeaseUntil-1); c != nil || err != nil {
		t.Fatalf("claimed under a lease: %+v, %v", c, err)
	}
	// The lease expired: another pusher takes the copy over.
	next, err := s.Claim(ctx, dc, time.Minute, old.LeaseUntil)
	if err != nil || next == nil || next.Attempts != 2 {
		t.Fatalf("Claim of an expired lease: %+v, %v", next, err)
	}
	// The first pusher changes nothing any more.
	stale := *old
	if ok, err := s.ExtendLease(ctx, &stale, time.Minute, now+200_000); ok || err != nil {
		t.Fatalf("a lost claim extended its lease: %v, %v", ok, err)
	}
	if ok, err := s.MarkReady(ctx, old, []byte("x"), now+200_000); ok || err != nil {
		t.Fatalf("a lost claim made the copy ready: %v, %v", ok, err)
	}
	if got, _ := s.Get(ctx, f.Key); got.Hashes != nil {
		t.Fatal("a lost claim stored hashes")
	}
	if ok, err := s.MarkFailed(ctx, old, "short_read", now+300_000, now+200_000); ok || err != nil {
		t.Fatalf("a lost claim failed the copy: %v, %v", ok, err)
	}
	if r, _ := s.Replica(ctx, f.RowId, dc); r.State != StatePushing || r.Attempts != 2 {
		t.Fatalf("copy after the lost claim's changes: %+v", r)
	}
	if ok, err := s.MarkReady(ctx, next, nil, now+200_000); !ok || err != nil {
		t.Fatalf("MarkReady of the holder: %v, %v", ok, err)
	}
}

func suiteFailures(t *testing.T, s Store) {
	ctx := context.Background()
	dc := suiteTarget()
	now := int64(30_000_000)
	f := ensure(t, s, FileKey{Kind: KindEncrypted, Id: suiteId()}, 2<<20)
	if err := s.Enqueue(ctx, f.RowId, dc, 0, now); err != nil {
		t.Fatal(err)
	}
	for attempt := int32(1); ; attempt++ {
		c, err := s.Claim(ctx, dc, time.Minute, now)
		if err != nil || c == nil || c.Attempts != attempt {
			t.Fatalf("claim %d: %+v, %v", attempt, c, err)
		}
		retry, again := RetryAt(c.Attempts, now)
		if !again {
			retry = 0
		}
		if ok, err := s.MarkFailed(ctx, c, "short_read", retry, now); !ok || err != nil {
			t.Fatalf("MarkFailed %d: %v, %v", attempt, ok, err)
		}
		r, _ := s.Replica(ctx, f.RowId, dc)
		if !again {
			if r.State != StateFailed || attempt != MaxPushAttempts || r.LastError != "short_read" {
				t.Fatalf("after %d attempts: %+v", attempt, r)
			}
			break
		}
		if r.State != StateQueued || r.NextTryAt != retry || r.LeaseUntil != 0 {
			t.Fatalf("after attempt %d: %+v", attempt, r)
		}
		// Backed off: not claimed before next_try_at.
		if c, err := s.Claim(ctx, dc, time.Minute, retry-1); c != nil || err != nil {
			t.Fatalf("claimed during the backoff: %+v, %v", c, err)
		}
		now = retry
	}
	// Failed for good: never claimed, and Enqueue leaves it.
	if err := s.Enqueue(ctx, f.RowId, dc, PriorityFirst, now); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Claim(ctx, dc, time.Minute, now+1<<40); c != nil || err != nil {
		t.Fatalf("a failed copy was claimed: %+v, %v", c, err)
	}
	if counts, _ := s.QueueCounts(ctx, dc); counts[StateFailed] != 1 {
		t.Fatalf("QueueCounts: %v", counts)
	}
	// A long reason is cut to the column.
	g := ensure(t, s, FileKey{Kind: KindEncrypted, Id: suiteId()}, 2<<20)
	if err := s.Enqueue(ctx, g.RowId, dc, 0, now); err != nil {
		t.Fatal(err)
	}
	c, err := s.Claim(ctx, dc, time.Minute, now)
	if err != nil || c == nil {
		t.Fatal(c, err)
	}
	if ok, err := s.MarkFailed(ctx, c, strings.Repeat("e", 100), now+1, now); !ok || err != nil {
		t.Fatalf("MarkFailed with a long reason: %v, %v", ok, err)
	}
	if r, _ := s.Replica(ctx, g.RowId, dc); len(r.LastError) != maxLastError {
		t.Fatalf("reason of %d bytes", len(r.LastError))
	}
}

// suitePriorityFirst: a preparation queued with PriorityFirst (here after
// MarkAbsent) is claimed before the queue.
func suitePriorityFirst(t *testing.T, s Store) {
	ctx := context.Background()
	dc := suiteTarget()
	const now = 40_000_000
	lost := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	waiting := []*File{
		ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20),
		ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20),
	}
	if err := s.Enqueue(ctx, lost.RowId, dc, 0, now); err != nil {
		t.Fatal(err)
	}
	c, err := s.Claim(ctx, dc, time.Minute, now)
	if err != nil || c == nil {
		t.Fatal(c, err)
	}
	if ok, err := s.MarkReady(ctx, c, nil, now); !ok || err != nil {
		t.Fatal(ok, err)
	}
	for _, f := range waiting {
		if err := s.Enqueue(ctx, f.RowId, dc, 0, now); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := s.MarkAbsent(ctx, lost.RowId, dc, now+1); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := s.Enqueue(ctx, lost.RowId, dc, PriorityFirst, now+1); err != nil {
		t.Fatal(err)
	}
	c, err = s.Claim(ctx, dc, time.Minute, now+2)
	if err != nil || c == nil || c.File.RowId != lost.RowId || c.Priority != PriorityFirst || c.Attempts != 1 {
		t.Fatalf("first claim after the raise: %+v, %v", c, err)
	}
	// A queued preparation is raised too, and not lowered after.
	if err := s.Enqueue(ctx, waiting[1].RowId, dc, PriorityFirst, now+3); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, waiting[1].RowId, dc, 0, now+3); err != nil {
		t.Fatal(err)
	}
	c, err = s.Claim(ctx, dc, time.Minute, now+4)
	if err != nil || c == nil || c.File.RowId != waiting[1].RowId {
		t.Fatalf("raised copy not first: %+v, %v", c, err)
	}
	if ok, err := s.MarkReady(ctx, c, nil, now+5); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if r, _ := s.Replica(ctx, waiting[1].RowId, dc); r.Priority != 0 {
		t.Fatalf("a ready copy kept its priority: %+v", r)
	}
}

// suiteDeliveries: the health of the deliveries and their traffic.
func suiteDeliveries(t *testing.T, s Store) {
	ctx := context.Background()
	dc, other := suiteDelivery(), suiteDelivery()
	if h, err := s.DeliveryHealth(ctx, dc); h != nil || err != nil {
		t.Fatalf("health before any check: %+v, %v", h, err)
	}
	put := &DeliveryHealth{DcId: dc, HealthyUntil: 1_000_180_000, UpdatedAt: 1_000_000_000}
	if err := s.PutDeliveryHealth(ctx, put); err != nil {
		t.Fatal(err)
	}
	if h, err := s.DeliveryHealth(ctx, dc); err != nil || h == nil || *h != *put {
		t.Fatalf("health: %+v, %v", h, err)
	}
	// Out, with a reason cut to the column.
	put = &DeliveryHealth{DcId: dc, UnhealthyReason: strings.Repeat("r", 40), UpdatedAt: 1_000_060_000}
	if err := s.PutDeliveryHealth(ctx, put); err != nil {
		t.Fatal(err)
	}
	h, err := s.DeliveryHealth(ctx, dc)
	if err != nil || h.HealthyUntil != 0 || len(h.UnhealthyReason) != maxUnhealthyReason || h.UpdatedAt != 1_000_060_000 {
		t.Fatalf("health out: %+v, %v", h, err)
	}
	if h, err := s.DeliveryHealth(ctx, other); h != nil || err != nil {
		t.Fatalf("health of another delivery: %+v, %v", h, err)
	}

	const month, next = "2026-11", "2026-12"
	if u, err := s.Usage(ctx, dc, month); err != nil || u != (DeliveryUsage{DcId: dc, Month: month}) {
		t.Fatalf("usage before any: %+v, %v", u, err)
	}
	for _, add := range [][2]int64{{100, 0}, {0, 3 << 20}, {5, 7}} {
		if err := s.AddUsage(ctx, dc, month, add[0], add[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddUsage(ctx, dc, next, 1, 1); err != nil {
		t.Fatal(err)
	}
	if u, err := s.Usage(ctx, dc, month); err != nil || u.OriginBytes != 105 || u.EdgeBytesEst != 3<<20+7 {
		t.Fatalf("usage: %+v, %v", u, err)
	}
	if u, err := s.Usage(ctx, dc, next); err != nil || u.OriginBytes != 1 || u.EdgeBytesEst != 1 {
		t.Fatalf("usage of the next month: %+v, %v", u, err)
	}
	if u, err := s.Usage(ctx, other, month); err != nil || u.OriginBytes != 0 {
		t.Fatalf("usage of another delivery: %+v, %v", u, err)
	}
	for _, bad := range []struct {
		month          string
		origin, edgeEs int64
	}{{"2026-13", 1, 1}, {"2026-1", 1, 1}, {"", 1, 1}, {month, -1, 0}, {month, 0, -1}} {
		if err := s.AddUsage(ctx, dc, bad.month, bad.origin, bad.edgeEs); err == nil {
			t.Errorf("AddUsage(%q, %d, %d) taken", bad.month, bad.origin, bad.edgeEs)
		}
	}

	// Concurrent additions all count.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.AddUsage(ctx, other, month, 10, 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if u, err := s.Usage(ctx, other, month); err != nil || u.OriginBytes != 80 || u.EdgeBytesEst != 8 {
		t.Fatalf("concurrent usage: %+v, %v", u, err)
	}
	if got := UsageMonth(time.Date(2026, 12, 1, 0, 30, 0, 0, time.FixedZone("", 3600))); got != "2026-11" {
		t.Fatalf("UsageMonth of 00:30 on Dec 1 at UTC+1: %s", got)
	}
}

// suiteConcurrent races the changes that must happen once.
func suiteConcurrent(t *testing.T, s Store) {
	ctx := context.Background()
	const racers = 8

	// SetKey: one key and token, the same for everyone.
	f := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	got := make([]*File, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sec, err := NewSecrets()
			if err != nil {
				t.Error(err)
				return
			}
			if got[i], err = s.SetKey(ctx, f.RowId, sec); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := 1; i < racers; i++ {
		if got[i] == nil || got[0] == nil || !bytes.Equal(got[i].Secrets.Token, got[0].Secrets.Token) || !bytes.Equal(got[i].Secrets.Key, got[0].Secrets.Key) {
			t.Fatal("racing SetKey calls saw different keys")
		}
	}

	// Ensure: one row.
	key := FileKey{Kind: KindPhoto, Id: suiteId(), Thumb: "w"}
	rows := make([]int64, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Ensure(ctx, &File{Key: key, Size: 4 << 20, Eligible: true, CreatedAt: 1})
			if err != nil || r == nil {
				t.Error(r, err)
				return
			}
			rows[i] = r.RowId
		}()
	}
	wg.Wait()
	for i := 1; i < racers; i++ {
		if rows[i] != rows[0] {
			t.Fatalf("racing Ensure calls made rows %v", rows)
		}
	}

	// Claim: two preparers on one preparation — one claim, the other waits
	// for the lease.
	dc := suiteTarget()
	const now = 60_000_000
	if err := s.Enqueue(ctx, f.RowId, dc, 0, now); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.Claim(ctx, dc, time.Minute, now)
			if err != nil {
				t.Error(err)
			}
			if c != nil {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := claims.Load(); n != 1 {
		t.Fatalf("%d preparers claimed one preparation", n)
	}
	// Two preparations, two preparers: each gets one.
	g := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	h := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	dc2 := suiteTarget()
	for _, x := range []*File{g, h} {
		if err := s.Enqueue(ctx, x.RowId, dc2, 0, now); err != nil {
			t.Fatal(err)
		}
	}
	held := make([]*Claim, 2)
	for i := range held {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if held[i], err = s.Claim(ctx, dc2, time.Minute, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if held[0] == nil || held[1] == nil || held[0].File.RowId == held[1].File.RowId {
		t.Fatalf("two preparers on two preparations: %+v", held)
	}
}
