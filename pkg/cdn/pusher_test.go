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
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// queue queues the file id of size bytes as a request of upload.getFile
// would.
func (e *homeEnv) queue(id int64, size int) {
	e.t.Helper()
	e.files.add(FileKey{Kind: KindDocument, Id: id}, randomBytes(e.t, size))
	if _, result := e.home.Redirect(e.ctx, docLoc(id), 0); result != ResultNotReady {
		e.t.Fatalf("file %d: %q, want not_ready", id, result)
	}
}

// TestPushShortRead: dfs answering fewer bytes than the file has fails the
// push with a backoff, and nothing is ready; after MaxPushAttempts the copy
// is failed for good.
func TestPushShortRead(t *testing.T) {
	e := newHomeEnv(t)
	e.queue(40, 3<<20)
	e.files.short[FileKey{Kind: KindDocument, Id: 40}] = true

	if !e.push.pushNext(e.ctx) {
		t.Fatal("nothing was claimed")
	}
	r := e.replica(40)
	if r.State != StateQueued || r.LastError != pushShortRead || r.NextTryAt != e.clock.now().Add(time.Minute).UnixMilli() {
		t.Fatalf("copy %+v, want queued again in a minute after a short read", r)
	}
	if e.node.puts != 1 || len(e.node.files) != 0 {
		t.Fatalf("%d puts, %d files on the node", e.node.puts, len(e.node.files))
	}
	if e.push.pushNext(e.ctx) {
		t.Fatal("claimed again before the backoff")
	}
	for attempt := 2; attempt <= MaxPushAttempts; attempt++ {
		e.clock.add(3 * time.Hour)
		if !e.push.pushNext(e.ctx) {
			t.Fatalf("attempt %d was not claimed", attempt)
		}
	}
	if r = e.replica(40); r.State != StateFailed || r.NextTryAt != 0 {
		t.Fatalf("copy %+v after %d attempts, want failed", r, MaxPushAttempts)
	}
	e.clock.add(24 * time.Hour)
	if e.push.pushNext(e.ctx) {
		t.Fatal("a failed copy was claimed")
	}
	if res, _ := e.home.Redirect(e.ctx, docLoc(40), 0); res != nil {
		t.Fatal("a file whose copy failed was redirected")
	}
}

// TestPushMismatch: a node that stored other bytes than were sent does not
// make the copy ready.
func TestPushMismatch(t *testing.T) {
	e := newHomeEnv(t)
	e.queue(41, 2<<20)
	e.node.corrupt = true
	e.push.pushNext(e.ctx)
	if r := e.replica(41); r.State != StateQueued || r.LastError != pushMismatch {
		t.Fatalf("copy %+v, want queued again after a mismatch", r)
	}
	f, err := e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: 41})
	if err != nil || f.Hashes != nil {
		t.Fatalf("hashes stored for a copy that is not ready: %v", err)
	}

	// The next attempt, with the node well, keeps the key and token of the
	// first: one cipher per file.
	token := append([]byte(nil), f.Secrets.Token...)
	e.node.corrupt = false
	e.clock.add(time.Minute)
	e.push.pushNext(e.ctx)
	if r := e.replica(41); r.State != StateReady {
		t.Fatalf("copy %+v, want ready", r)
	}
	if f, _ = e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: 41}); string(f.Secrets.Token) != string(token) {
		t.Fatal("the token changed between attempts")
	}
}

// TestPushNodeFull: a node out of room fails the push with a backoff; a
// file larger than its cache is not pushed at all.
func TestPushNodeFull(t *testing.T) {
	e := newHomeEnv(t)
	e.queue(42, 2<<20)
	e.node.putErr = status.Error(codes.ResourceExhausted, "the disk is below its reserve")
	e.push.pushNext(e.ctx)
	if r := e.replica(42); r.State != StateQueued || r.LastError != pushNodeFull {
		t.Fatalf("copy %+v, want queued again", r)
	}

	e.node.putErr = nil
	e.push.cap = func() int64 { return 1 << 20 }
	e.clock.add(time.Minute)
	e.push.pushNext(e.ctx)
	if r := e.replica(42); r.State != StateFailed || r.LastError != pushTooLarge {
		t.Fatalf("copy %+v, want failed as too large", r)
	}
	if e.node.puts != 1 {
		t.Fatalf("%d puts; a file larger than the cache was sent", e.node.puts)
	}
}

// TestPushRate: the pushes keep to CDN_PUSH_RATE_MBIT.
func TestPushRate(t *testing.T) {
	e := newHomeEnv(t)
	const bytesPerSecond = 4 << 20
	e.push.limiter = rate.NewLimiter(rate.Limit(bytesPerSecond), PushChunkSize)
	e.queue(43, 5<<20)
	start := time.Now()
	e.push.pushNext(e.ctx)
	elapsed := time.Since(start)
	if r := e.replica(43); r.State != StateReady {
		t.Fatalf("copy %+v, want ready", r)
	}
	// The first megabyte is the burst; the other four take a second.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("5 MiB pushed in %v at %d bytes/s", elapsed, bytesPerSecond)
	}
}

// TestPusherRun: the workers push what is queued, a wake makes them look
// at once, and they stop with their context.
func TestPusherRun(t *testing.T) {
	e := newHomeEnv(t)
	e.push.idle = time.Hour
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.push.Run(ctx)
	}()
	e.home.wake = e.push.Wake
	e.queue(44, 2<<20)
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: 44})
		if err != nil {
			t.Fatal(err)
		}
		r, err := e.store.Replica(e.ctx, f.RowId, testDc)
		if err != nil {
			t.Fatal(err)
		}
		if r.State == StateReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("copy %+v not pushed after a wake", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}
