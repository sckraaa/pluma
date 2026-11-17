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
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeTarget is a Target that keeps what it is given.
type fakeTarget struct {
	id     int32
	cipher bool
	// failAfter makes Put fail after reading so many bytes (-1: never).
	failAfter int64

	mu      sync.Mutex
	puts    int
	objects map[string][]byte
}

func newFakeTarget(id int32, cipher bool) *fakeTarget {
	return &fakeTarget{id: id, cipher: cipher, failAfter: -1, objects: map[string][]byte{}}
}

func (f *fakeTarget) ID() int32             { return f.id }
func (f *fakeTarget) NeedsCiphertext() bool { return f.cipher }

func (f *fakeTarget) Put(_ context.Context, name string, r io.Reader, size int64) error {
	f.mu.Lock()
	f.puts++
	f.mu.Unlock()
	var b []byte
	var err error
	if f.failAfter >= 0 {
		b, err = io.ReadAll(io.LimitReader(r, f.failAfter))
		if err == nil {
			err = errors.New("the store refused the object")
		}
	} else {
		b, err = io.ReadAll(r)
	}
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return errors.New("a short object")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[name] = b
	return nil
}

// prepEnv is a Preparer over a MemStore and files in memory.
type prepEnv struct {
	t      *testing.T
	ctx    context.Context
	set    *Settings
	store  *MemStore
	reader *memReader
	clock  *clock
	ready  []FileKey
	mu     sync.Mutex
}

func newPrepEnv(t *testing.T) *prepEnv {
	t.Helper()
	return &prepEnv{t: t, ctx: context.Background(), set: testSettings(t), store: NewMemStore(), reader: newMemReader(), clock: newClock()}
}

func (e *prepEnv) preparer(target Target) *Preparer {
	p := NewPreparer(e.store, target, e.reader, e.set, func(k FileKey) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.ready = append(e.ready, k)
	})
	p.now = e.clock.now
	return p
}

// queue makes the row of a file of key with data and queues it for target.
func (e *prepEnv) queue(key FileKey, data []byte, target int32) *File {
	e.t.Helper()
	e.reader.add(key, data)
	f, err := e.store.Ensure(e.ctx, &File{Key: key, Size: int64(len(data)), Eligible: Eligible(int64(len(data)), e.set.MinFileSize)})
	if err != nil {
		e.t.Fatal(err)
	}
	if err = e.store.Enqueue(e.ctx, f.RowId, target, 0, e.clock.now().UnixMilli()); err != nil {
		e.t.Fatal(err)
	}
	return f
}

func (e *prepEnv) replica(rowId int64, target int32) *Replica {
	e.t.Helper()
	r, err := e.store.Replica(e.ctx, rowId, target)
	if err != nil || r == nil {
		e.t.Fatalf("Replica: %v, %v", r, err)
	}
	return r
}

func TestPreparerHomeOrigin(t *testing.T) {
	e := newPrepEnv(t)
	data := refFile()
	f := e.queue(docKey(1), data, TargetHomeOrigin)
	// The home origin is the target, but a fake one shows that nothing is
	// put anywhere.
	target := newFakeTarget(TargetHomeOrigin, false)
	p := e.preparer(target)
	if !p.Next(e.ctx) {
		t.Fatal("no preparation claimed")
	}
	got, err := e.store.Get(e.ctx, f.Key)
	if err != nil {
		t.Fatal(err)
	}
	want := NewHasher()
	_, _ = want.Write(data)
	if !got.HasKey() || !bytes.Equal(got.Hashes, want.Sum()) {
		t.Errorf("hashes %d bytes (want %d), key %v", len(got.Hashes), len(want.Sum()), got.HasKey())
	}
	if r := e.replica(f.RowId, TargetHomeOrigin); r.State != StateReady {
		t.Errorf("state %d, want ready", r.State)
	}
	if target.puts != 0 {
		t.Errorf("%d puts for the home origin", target.puts)
	}
	if len(e.ready) != 1 || e.ready[0] != f.Key {
		t.Errorf("onReady: %v", e.ready)
	}
	if p.Next(e.ctx) {
		t.Error("a second preparation claimed")
	}
	// The hashes answer the clients: upload.getCdnFileHashes of part 9.
	hashes, err := Answer(got.Hashes, got.Size, 9*HashPartSize)
	if err != nil || len(hashes) == 0 {
		t.Fatalf("Answer: %v", err)
	}
	sum := NewHasher()
	_, _ = sum.Write(data[9*HashPartSize : 10*HashPartSize])
	if !bytes.Equal(hashes[0].GetHash(), sum.Sum()) {
		t.Error("the hash of part 9 is not of its plaintext")
	}
}

func TestPreparerKeepsKey(t *testing.T) {
	e := newPrepEnv(t)
	f := e.queue(docKey(1), refFile(), TargetHomeOrigin)
	sec := mustSecrets(t)
	if _, err := e.store.SetKey(e.ctx, f.RowId, sec); err != nil {
		t.Fatal(err)
	}
	e.preparer(HomeOrigin{}).Next(e.ctx)
	got, _ := e.store.Get(e.ctx, f.Key)
	if !bytes.Equal(got.Secrets.Token, sec.Token) || !bytes.Equal(got.Secrets.Key, sec.Key) || got.Hashes == nil {
		t.Error("the key set first did not stay")
	}
}

func TestPreparerFailures(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(e *prepEnv)
		result    string
		failedFor bool // failed for good, not queued again
	}{
		{"short read", func(e *prepEnv) { e.reader.shortAt = 1 << 20 }, prepareShortRead, false},
		{"read error", func(e *prepEnv) { e.reader.failAt = 2 << 20 }, prepareReadError, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newPrepEnv(t)
			c.setup(e)
			f := e.queue(docKey(1), refFile(), TargetHomeOrigin)
			e.preparer(HomeOrigin{}).Next(e.ctx)
			r := e.replica(f.RowId, TargetHomeOrigin)
			next, _ := RetryAt(1, e.clock.now().UnixMilli())
			if r.State != StateQueued || r.LastError != c.result || r.NextTryAt != next {
				t.Errorf("state %d, last error %q, next try %d; want queued, %q, %d", r.State, r.LastError, r.NextTryAt, c.result, next)
			}
			if got, _ := e.store.Get(e.ctx, f.Key); got.Hashes != nil {
				t.Error("hashes of a failed preparation")
			}
			if len(e.ready) != 0 {
				t.Error("onReady of a failed preparation")
			}
		})
	}

	t.Run("not eligible", func(t *testing.T) {
		e := newPrepEnv(t)
		f := e.queue(docKey(1), make([]byte, 1000), TargetHomeOrigin)
		e.preparer(HomeOrigin{}).Next(e.ctx)
		if r := e.replica(f.RowId, TargetHomeOrigin); r.State != StateFailed || r.LastError != prepareBadSize {
			t.Errorf("state %d, %q; want failed for good, bad_size", r.State, r.LastError)
		}
	})

	t.Run("store down", func(t *testing.T) {
		e := newPrepEnv(t)
		e.queue(docKey(1), refFile(), TargetHomeOrigin)
		e.store.SetErr(errors.New("mysql is down"))
		if e.preparer(HomeOrigin{}).Next(e.ctx) {
			t.Error("a claim with the store down")
		}
	})
}

// TestPreparerCiphertext checks the cipher a push target gets: the file
// encrypted from its first byte, under the name of its id.
func TestPreparerCiphertext(t *testing.T) {
	e := newPrepEnv(t)
	data := refFile()
	f := e.queue(docKey(1), data, 2)
	target := newFakeTarget(2, true)
	e.preparer(target).Next(e.ctx)
	got, _ := e.store.Get(e.ctx, f.Key)
	if r := e.replica(f.RowId, 2); r.State != StateReady {
		t.Fatalf("state %d (%q), want ready", r.State, r.LastError)
	}
	obj, ok := target.objects[ObjectPath(got.Secrets.Token)[1:]]
	if !ok || !bytes.Equal(obj, cipherOf(t, got, data)) {
		t.Errorf("the target has %d bytes under the name, want the cipher of %d", len(obj), len(data))
	}
}

func TestPreparerPutFails(t *testing.T) {
	for _, failAfter := range []int64{0, 1 << 20, 1<<20 + 5} {
		e := newPrepEnv(t)
		f := e.queue(docKey(1), refFile(), 2)
		target := newFakeTarget(2, true)
		target.failAfter = failAfter
		done := make(chan struct{})
		go func() {
			defer close(done)
			e.preparer(target).Next(e.ctx)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("fail after %d: the preparation hangs", failAfter)
		}
		r := e.replica(f.RowId, 2)
		if r.State != StateQueued || r.LastError != preparePutError {
			t.Errorf("fail after %d: state %d, %q; want queued, put_error", failAfter, r.State, r.LastError)
		}
		if len(target.objects) != 0 {
			t.Errorf("fail after %d: an object was kept", failAfter)
		}
	}
}

// TestPreparerReadFailureAbortsPut checks that a failed read ends the Put
// with an error rather than a short object.
func TestPreparerReadFailureAbortsPut(t *testing.T) {
	e := newPrepEnv(t)
	e.reader.failAt = 2 << 20
	f := e.queue(docKey(1), refFile(), 2)
	target := newFakeTarget(2, true)
	e.preparer(target).Next(e.ctx)
	if r := e.replica(f.RowId, 2); r.State != StateQueued || r.LastError != prepareReadError {
		t.Errorf("state %d, %q; want queued, read_error", r.State, r.LastError)
	}
	if target.puts != 1 || len(target.objects) != 0 {
		t.Errorf("%d puts, %d objects kept", target.puts, len(target.objects))
	}
}

func TestPreparerRate(t *testing.T) {
	e := newPrepEnv(t)
	// 8 Mbit/s is 10^6 bytes a second: a burst of one part, then the
	// second part (1 MiB) waits about a second.
	e.set.PrepareBytesPerSecond = 1_000_000
	f := e.queue(docKey(1), make([]byte, 2<<20+1), TargetHomeOrigin)
	start := time.Now()
	e.preparer(HomeOrigin{}).Next(e.ctx)
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Errorf("2 MiB prepared in %v at 10^6 bytes a second", d)
	}
	if r := e.replica(f.RowId, TargetHomeOrigin); r.State != StateReady {
		t.Errorf("state %d", r.State)
	}
}

// TestPreparerSlowRead checks that a dfs slower than the rate still
// prepares a file: the bound of the whole preparation does not come from
// the rate.
func TestPreparerSlowRead(t *testing.T) {
	e := newPrepEnv(t)
	// At 800 Mbit/s 4 MiB take 42 ms; dfs gives them in about 200 ms.
	e.reader.delay = 50 * time.Millisecond
	f := e.queue(docKey(1), make([]byte, 4<<20), TargetHomeOrigin)
	p := e.preparer(HomeOrigin{})
	p.slack = 50 * time.Millisecond
	p.Next(e.ctx)
	if r := e.replica(f.RowId, TargetHomeOrigin); r.State != StateReady {
		t.Errorf("state %d (%q), want ready", r.State, r.LastError)
	}
}

// TestPreparerPartTimeout checks that a part dfs does not give is a
// timeout, tried again.
func TestPreparerPartTimeout(t *testing.T) {
	e := newPrepEnv(t)
	block := make(chan struct{})
	defer close(block)
	e.reader.block = block
	f := e.queue(docKey(1), refFile(), TargetHomeOrigin)
	p := e.preparer(HomeOrigin{})
	p.partTimeout = 50 * time.Millisecond
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Next(e.ctx)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a stalled part holds the preparation")
	}
	r := e.replica(f.RowId, TargetHomeOrigin)
	next, _ := RetryAt(1, e.clock.now().UnixMilli())
	if r.State != StateQueued || r.LastError != prepareTimeout || r.NextTryAt != next {
		t.Errorf("state %d, %q, next try %d; want queued, timeout, %d", r.State, r.LastError, r.NextTryAt, next)
	}
}

func TestPreparerStop(t *testing.T) {
	e := newPrepEnv(t)
	block := make(chan struct{})
	e.reader.block = block
	f := e.queue(docKey(1), refFile(), TargetHomeOrigin)
	ctx, cancel := context.WithCancel(e.ctx)
	p := e.preparer(HomeOrigin{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run does not stop")
	}
	close(block)
	// Cut short, not failed: claimed again once its lease runs out.
	if r := e.replica(f.RowId, TargetHomeOrigin); r.State != StatePushing || r.LastError != "" {
		t.Errorf("state %d, %q; want still claimed", r.State, r.LastError)
	}
	e.clock.add(prepareLease)
	if !e.preparer(HomeOrigin{}).Next(e.ctx) {
		t.Fatal("the expired lease was not taken")
	}
	if r := e.replica(f.RowId, TargetHomeOrigin); r.State != StateReady {
		t.Errorf("state %d after the second preparer", r.State)
	}
}

func TestPreparerWake(t *testing.T) {
	e := newPrepEnv(t)
	p := e.preparer(HomeOrigin{})
	p.idle = time.Hour
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	go p.Run(ctx)
	time.Sleep(20 * time.Millisecond)
	f := e.queue(docKey(1), refFile(), TargetHomeOrigin)
	p.Wake()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, _ := e.store.Replica(e.ctx, f.RowId, TargetHomeOrigin); r != nil && r.State == StateReady {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("a woken preparer did not take the file")
}

func TestTargetOf(t *testing.T) {
	set := testSettings(t)
	tg, err := targetOf(&set.Deliveries[0])
	if err != nil || tg.ID() != TargetHomeOrigin || tg.NeedsCiphertext() {
		t.Errorf("home-origin: %v, %v", tg, err)
	}
	if err := (HomeOrigin{}).Put(context.Background(), "o/x", bytes.NewReader(nil), 0); err == nil {
		t.Error("Put of the home origin")
	}
	d := set.Deliveries[0]
	d.Source = "push-store"
	if _, err := targetOf(&d); err == nil {
		t.Error("a target for an unknown source")
	}
}
