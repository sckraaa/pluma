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

// suiteSeq makes file ids and DCs unique across tests and runs on one
// database (go test -count=N).
var suiteSeq atomic.Int64

func suiteId() int64 { return time.Now().UnixMilli()%1_000_000_000*1000 + suiteSeq.Add(1) }

// suiteDc is a DC of its own for a test, so the queues of tests do not mix.
func suiteDc() int32 { return int32(1000 + suiteId()%1_000_000_000) }

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
	t.Run("reupload first", func(t *testing.T) { suiteReuploadFirst(t, s) })
	t.Run("nodes", func(t *testing.T) { suiteNodes(t, s) })
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
	dc := suiteDc()
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
	if err = s.Enqueue(ctx, a.RowId, dc, PriorityReupload, now+6); err != nil {
		t.Fatal(err)
	}
	if r, _ = s.Replica(ctx, a.RowId, dc); r.State != StateReady || r.Priority != 0 {
		t.Fatalf("Enqueue changed a ready copy: %+v", r)
	}
	ready, err := s.ListReady(ctx, dc, 0, 10)
	if err != nil || len(ready) != 1 || ready[0].FileRowId != a.RowId || !bytes.Equal(ready[0].Token, sec.Token) || ready[0].Size != 2<<20 {
		t.Fatalf("ListReady: %+v, %v", ready, err)
	}
	if ready, err = s.ListReady(ctx, dc, a.RowId, 10); err != nil || len(ready) != 0 {
		t.Fatalf("ListReady after the last: %+v, %v", ready, err)
	}

	// The node lost it: absent, not queued until asked for again.
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
	dc := suiteDc()
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
	dc := suiteDc()
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
	if err := s.Enqueue(ctx, f.RowId, dc, PriorityReupload, now); err != nil {
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

// suiteReuploadFirst: a copy upload.reuploadCdnFile asked for (MarkAbsent,
// Enqueue with PriorityReupload) is pushed before the queue.
func suiteReuploadFirst(t *testing.T, s Store) {
	ctx := context.Background()
	dc := suiteDc()
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
	if err := s.Enqueue(ctx, lost.RowId, dc, PriorityReupload, now+1); err != nil {
		t.Fatal(err)
	}
	c, err = s.Claim(ctx, dc, time.Minute, now+2)
	if err != nil || c == nil || c.File.RowId != lost.RowId || c.Priority != PriorityReupload || c.Attempts != 1 {
		t.Fatalf("first claim after the reupload: %+v, %v", c, err)
	}
	// A queued copy is raised by a reupload too, and not lowered after.
	if err := s.Enqueue(ctx, waiting[1].RowId, dc, PriorityReupload, now+3); err != nil {
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

func suiteNodes(t *testing.T, s Store) {
	ctx := context.Background()
	dc := suiteDc()
	const now = 50_000_000
	const every = 30 * 24 * time.Hour
	if n, err := s.Node(ctx, dc); n != nil || err != nil {
		t.Fatalf("Node before any: %v, %v", n, err)
	}
	k1, k2, k3 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32)
	n, err := s.RotateRtKey(ctx, dc, k1, every, now)
	if err != nil || !bytes.Equal(n.RtKey, k1) || n.RtKeyPrev != nil || n.RtKeyAt != now {
		t.Fatalf("first key: %+v, %v", n, err)
	}
	if n, err = s.RotateRtKey(ctx, dc, k2, every, now+every.Milliseconds()-1); err != nil || !bytes.Equal(n.RtKey, k1) || n.RtKeyPrev != nil {
		t.Fatalf("rotated before its time: %+v, %v", n, err)
	}
	if _, err = s.RotateRtKey(ctx, dc, k2[:16], every, now); err == nil {
		t.Fatal("RotateRtKey took a 16-byte key")
	}

	put := &Node{
		DcId: dc, PublicKey: "-----BEGIN RSA PUBLIC KEY-----\nAAAA\n-----END RSA PUBLIC KEY-----\n", Fingerprint: -42,
		BootId: bytes.Repeat([]byte{5}, 16), HealthyUntil: now + 30_000, EgressMonth: "2026-11", EgressBytes: 7 << 30,
		CacheUsed: 1 << 30, CacheCap: 50 << 30, UpdatedAt: now,
		RtKey: k3, // not PutNode's to change
	}
	if err = s.PutNode(ctx, put); err != nil {
		t.Fatal(err)
	}
	n, err = s.Node(ctx, dc)
	if err != nil || n.PublicKey != put.PublicKey || n.Fingerprint != -42 || !bytes.Equal(n.BootId, put.BootId) ||
		n.HealthyUntil != put.HealthyUntil || n.EgressMonth != "2026-11" || n.EgressBytes != 7<<30 ||
		n.CacheUsed != 1<<30 || n.CacheCap != 50<<30 || !bytes.Equal(n.RtKey, k1) || n.RtKeyAt != now {
		t.Fatalf("Node after PutNode: %+v, %v", n, err)
	}
	if keys := n.RtKeys(); len(keys) != 2 || !bytes.Equal(keys[0], k1) || keys[1] != nil {
		t.Fatalf("RtKeys: %v", keys)
	}
	// Unhealthy: the reason replaces the old state.
	put.HealthyUntil, put.UnhealthyReason, put.UpdatedAt = 0, "egress", now+10_000
	if err = s.PutNode(ctx, put); err != nil {
		t.Fatal(err)
	}
	if n, _ = s.Node(ctx, dc); n.HealthyUntil != 0 || n.UnhealthyReason != "egress" {
		t.Fatalf("unhealthy node: %+v", n)
	}

	n, err = s.RotateRtKey(ctx, dc, k2, every, now+every.Milliseconds())
	if err != nil || !bytes.Equal(n.RtKey, k2) || !bytes.Equal(n.RtKeyPrev, k1) || n.RtKeyAt != now+every.Milliseconds() {
		t.Fatalf("rotation: %+v, %v", n, err)
	}
	if n.PublicKey != put.PublicKey || n.Fingerprint != -42 {
		t.Fatalf("rotation changed the node: %+v", n)
	}

	// A node seen before it got a key gets one.
	dc2 := suiteDc()
	if err = s.PutNode(ctx, &Node{DcId: dc2, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if n, _ = s.Node(ctx, dc2); n.RtKey != nil || n.PublicKey != "" || n.BootId != nil {
		t.Fatalf("new node: %+v", n)
	}
	if n, err = s.RotateRtKey(ctx, dc2, k3, every, now); err != nil || !bytes.Equal(n.RtKey, k3) || n.RtKeyPrev != nil {
		t.Fatalf("key of a node without one: %+v, %v", n, err)
	}

	nodes, err := s.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var seen []int32
	for i, n := range nodes {
		if i > 0 && nodes[i-1].DcId >= n.DcId {
			t.Fatalf("Nodes not by DC: %d after %d", n.DcId, nodes[i-1].DcId)
		}
		if n.DcId == dc || n.DcId == dc2 {
			seen = append(seen, n.DcId)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("Nodes: %v of %d, %d", seen, dc, dc2)
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

	// Claim: two pushers on one copy — one push, the other waits for the
	// lease.
	dc := suiteDc()
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
		t.Fatalf("%d pushers claimed one copy", n)
	}
	// Two copies, two pushers: each gets one.
	g := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	h := ensure(t, s, FileKey{Kind: KindDocument, Id: suiteId()}, 2<<20)
	dc2 := suiteDc()
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
		t.Fatalf("two pushers on two copies: %+v", held)
	}
}
