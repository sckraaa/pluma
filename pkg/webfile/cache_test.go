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

package webfile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCache(t *testing.T, maxBytes int64) (*DiskCache, *time.Time) {
	t.Helper()
	c, err := NewDiskCache(t.TempDir(), maxBytes, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	c.now = func() time.Time { return now }
	return c, &now
}

// filler counts its fills and gives n bytes of b.
type filler struct {
	n     atomic.Int32
	b     byte
	size  int
	block chan struct{}
}

func (f *filler) fill(context.Context) (File, error) {
	f.n.Add(1)
	if f.block != nil {
		<-f.block
	}
	return File{Bytes: bytes.Repeat([]byte{f.b}, f.size), MimeType: "image/jpeg", Mtime: 9}, nil
}

func get(t *testing.T, c *DiskCache, key string, f *filler) []byte {
	t.Helper()
	h, err := c.Get(context.Background(), key, f.fill)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	b, err := h.Read(0, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if h.MimeType() != "image/jpeg" || h.Mtime() != 9 || h.Size() != int64(len(b)) {
		t.Fatalf("%s %d %d", h.MimeType(), h.Mtime(), h.Size())
	}
	return b
}

func cacheFiles(t *testing.T, c *DiskCache) int {
	t.Helper()
	names, err := os.ReadDir(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

func TestCacheKeepsAndEvicts(t *testing.T) {
	c, now := newTestCache(t, 250)
	a, b, d := &filler{b: 'a', size: 100}, &filler{b: 'b', size: 100}, &filler{b: 'd', size: 100}
	if got := get(t, c, "a", a); len(got) != 100 || got[0] != 'a' {
		t.Fatalf("%q", got)
	}
	get(t, c, "a", a)
	get(t, c, "b", b)
	if a.n.Load() != 1 || b.n.Load() != 1 || c.Bytes() != 200 {
		t.Fatalf("fills a %d b %d, %d bytes", a.n.Load(), b.n.Load(), c.Bytes())
	}
	*now = now.Add(time.Minute)
	get(t, c, "a", a) // a is now the most recent
	get(t, c, "d", d) // over the limit: b, the least recently used, goes
	if c.Bytes() != 200 || cacheFiles(t, c) != 2 {
		t.Fatalf("%d bytes, %d files", c.Bytes(), cacheFiles(t, c))
	}
	get(t, c, "a", a)
	get(t, c, "b", b)
	if a.n.Load() != 1 || b.n.Load() != 2 {
		t.Fatalf("fills a %d b %d", a.n.Load(), b.n.Load())
	}
}

func TestCacheTTL(t *testing.T) {
	c, now := newTestCache(t, 1000)
	a := &filler{b: 'a', size: 10}
	get(t, c, "a", a)
	*now = now.Add(59 * time.Minute)
	get(t, c, "a", a)
	*now = now.Add(time.Minute)
	get(t, c, "a", a)
	if a.n.Load() != 2 || cacheFiles(t, c) != 1 || c.Bytes() != 10 {
		t.Fatalf("%d fills, %d files, %d bytes", a.n.Load(), cacheFiles(t, c), c.Bytes())
	}
}

// Callers asking for a file meanwhile share one fill, and a caller that
// stops waiting does not stop it for the others.
func TestCacheOneFillPerKey(t *testing.T) {
	c, _ := newTestCache(t, 1000)
	f := &filler{b: 'x', size: 10, block: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := c.Get(ctx, "k", f.fill)
		first <- err
	}()
	for f.n.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := get(t, c, "k", f); len(got) != 10 {
				t.Errorf("%q", got)
			}
		}()
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("the caller that stopped waiting: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	close(f.block)
	wg.Wait()
	if f.n.Load() != 1 {
		t.Fatalf("%d fills", f.n.Load())
	}
}

func TestCacheFillError(t *testing.T) {
	c, _ := newTestCache(t, 1000)
	if _, err := c.Get(context.Background(), "k", func(context.Context) (File, error) { return File{}, ErrNotAvailable }); !errors.Is(err, ErrNotAvailable) {
		t.Fatal(err)
	}
	if c.Bytes() != 0 || cacheFiles(t, c) != 0 {
		t.Fatal("a failed fill kept")
	}
}

// A file evicted while a part of it is read stays readable until the read
// is done, and then goes.
func TestCacheEvictedWhileOpen(t *testing.T) {
	c, _ := newTestCache(t, 150)
	a := &filler{b: 'a', size: 100}
	h, err := c.Get(context.Background(), "a", a.fill)
	if err != nil {
		t.Fatal(err)
	}
	get(t, c, "b", &filler{b: 'b', size: 100})
	if c.Bytes() != 100 || cacheFiles(t, c) != 2 {
		t.Fatalf("%d bytes, %d files", c.Bytes(), cacheFiles(t, c))
	}
	if b, err := h.Read(90, 50); err != nil || len(b) != 10 || b[0] != 'a' {
		t.Fatalf("%q, %v", b, err)
	}
	h.Close()
	if cacheFiles(t, c) != 1 {
		t.Fatalf("%d files after the last read", cacheFiles(t, c))
	}
}

// A file larger than the whole cache is served and not kept.
func TestCacheTooLarge(t *testing.T) {
	c, _ := newTestCache(t, 50)
	f := &filler{b: 'z', size: 100}
	if got := get(t, c, "z", f); len(got) != 100 {
		t.Fatalf("%d bytes", len(got))
	}
	if c.Bytes() != 0 || cacheFiles(t, c) != 0 {
		t.Fatal("kept")
	}
}

// A start removes the files a previous run left, and only them.
func TestCacheStartRemovesLeftovers(t *testing.T) {
	dir := t.TempDir()
	for name, keep := range map[string]bool{
		"0123456789abcdef0123456789abcdef-7.wf": false,
		"fill-3.tmp":                            false,
		"notes.txt":                             true,
		"0123-7.wf":                             true,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, err := os.Stat(filepath.Join(dir, name))
			if (err == nil) != keep {
				t.Errorf("%s: kept %v, want %v", name, err == nil, keep)
			}
		}()
	}
	if _, err := NewDiskCache(dir, 100, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskCache(filepath.Join(dir, "sub", "dir"), 100, time.Hour); err != nil {
		t.Fatalf("a new directory: %v", err)
	}
}
