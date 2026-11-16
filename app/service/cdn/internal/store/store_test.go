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

package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// clock is a settable time for tests.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)} }

func plenty(string) (int64, error) { return 1 << 40, nil }

func token(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, cdn.TokenSize)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func data(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func open(t *testing.T, dir string, capBytes int64, c *clock) *Store {
	t.Helper()
	s, err := Open(Config{Dir: dir, CacheBytes: capBytes, IdleTTL: 720 * time.Hour, DiskReserve: 0, Now: c.Now, DiskFree: plenty})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// put stores b under tok in chunks and checks the answer.
func put(t *testing.T, s *Store, tok, b []byte) {
	t.Helper()
	w, err := s.Put(tok, int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for off := 0; off < len(b); off += 1000 {
		if _, err := w.Write(b[off:min(off+1000, len(b))]); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := w.Commit()
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(b)
	if !bytes.Equal(sum, want[:]) {
		t.Fatal("sha256 of the put differs")
	}
}

func has(s *Store, tok []byte) bool {
	_, _, _, err := s.ReadAt(tok, 0, 4096)
	return err == nil
}

func TestPutRead(t *testing.T) {
	s := open(t, t.TempDir(), 1<<30, newClock())
	tok := token(t)
	b := data(t, 3<<20+17)
	put(t, s, tok, b)

	for _, tc := range []struct {
		off   int64
		limit int
		want  []byte
	}{
		{0, 131072, b[:131072]},
		{1 << 20, 1 << 20, b[1<<20 : 2<<20]},
		{3 << 20, 1 << 20, b[3<<20:]}, // the last part is short
		{int64(len(b)), 4096, nil},    // at the end: nothing
		{4 << 20, 4096, nil},          // past it
	} {
		got, size, readErr, err := s.ReadAt(tok, tc.off, tc.limit)
		if err != nil || readErr || size != int64(len(b)) || !bytes.Equal(got, tc.want) {
			t.Errorf("ReadAt(%d, %d): %d bytes, size %d, %v, %v", tc.off, tc.limit, len(got), size, readErr, err)
		}
	}
	if _, _, _, err := s.ReadAt(token(t), 0, 4096); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown token: %v", err)
	}
	if used, n := s.Usage(); used != int64(len(b)) || n != 1 {
		t.Errorf("usage %d bytes, %d files", used, n)
	}
}

// TestPutBroken: a put cut short, aborted or overfilled leaves no file and
// no reserved space.
func TestPutBroken(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, 1<<20, newClock())

	tok := token(t)
	w, err := s.Put(tok, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data(t, 999)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(); !errors.Is(err, ErrSize) {
		t.Fatalf("commit of 999 of 1000 bytes: %v", err)
	}
	if has(s, tok) {
		t.Fatal("a short put is served")
	}

	w, err = s.Put(tok, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data(t, 11)); !errors.Is(err, ErrSize) {
		t.Fatalf("11 bytes into 10: %v", err)
	}
	w.Abort()

	w, err = s.Put(tok, 1<<20)
	if err != nil {
		t.Fatalf("the whole cache after aborted puts: %v", err)
	}
	if _, err := w.Write(data(t, 4096)); err != nil {
		t.Fatal(err)
	}
	w.Abort()

	left, err := os.ReadDir(filepath.Join(dir, tmpDir))
	if err != nil || len(left) != 0 {
		t.Fatalf("tmp after aborted puts: %d files, %v", len(left), err)
	}
	if used, n := s.Usage(); used != 0 || n != 0 {
		t.Fatalf("usage %d bytes, %d files", used, n)
	}
	if _, err := s.Put(tok, 1<<20+1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a file over the cache: %v", err)
	}
	if _, err := s.Put([]byte("short"), 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a short token: %v", err)
	}
}

// TestEvictLRU: a new file pushes out the least recently served ones.
func TestEvictLRU(t *testing.T) {
	c := newClock()
	s := open(t, t.TempDir(), 3*4096, c)
	a, b, d := token(t), token(t), token(t)
	put(t, s, a, data(t, 4096))
	c.Add(time.Second)
	put(t, s, b, data(t, 4096))
	c.Add(time.Second)
	put(t, s, d, data(t, 4096))
	c.Add(time.Second)
	if !has(s, a) { // a is now the most recently served
		t.Fatal("a is missing")
	}
	put(t, s, token(t), data(t, 4096))
	if has(s, b) {
		t.Fatal("b, the least recently served, was kept")
	}
	if !has(s, a) || !has(s, d) {
		t.Fatal("a recently served file was dropped")
	}
	if used, n := s.Usage(); used != 3*4096 || n != 3 {
		t.Fatalf("usage %d bytes, %d files", used, n)
	}
}

func TestDiskReserve(t *testing.T) {
	free := int64(10000)
	s, err := Open(Config{Dir: t.TempDir(), CacheBytes: 1 << 20, IdleTTL: time.Hour, DiskReserve: 8000,
		DiskFree: func(string) (int64, error) { return free, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(token(t), 3000); !errors.Is(err, ErrDiskLow) {
		t.Fatalf("a file that leaves less than the reserve: %v", err)
	}
	w, err := s.Put(token(t), 2000)
	if err != nil {
		t.Fatalf("a file that leaves the reserve: %v", err)
	}
	w.Abort()
	if _, low, _ := s.Disk(); low {
		t.Fatal("10000 free bytes reported below a reserve of 8000")
	}
	free = 7999
	if _, low, _ := s.Disk(); !low {
		t.Fatal("7999 free bytes not reported below a reserve of 8000")
	}
}

// TestIdle: a file not served for the idle time is dropped by Maintain.
func TestIdle(t *testing.T) {
	c := newClock()
	s, err := Open(Config{Dir: t.TempDir(), CacheBytes: 1 << 20, IdleTTL: 24 * time.Hour, Now: c.Now, DiskFree: plenty})
	if err != nil {
		t.Fatal(err)
	}
	old, fresh := token(t), token(t)
	put(t, s, old, data(t, 100))
	put(t, s, fresh, data(t, 100))
	c.Add(20 * time.Hour)
	has(s, fresh)
	c.Add(5 * time.Hour)
	if err := s.Maintain(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.ReadAt(old, 0, 4096); !errors.Is(err, ErrNotFound) {
		t.Fatal("a file idle for 25 hours of 24 was kept")
	}
	if !has(s, fresh) {
		t.Fatal("a file served 5 hours ago was dropped")
	}
}

// TestReopen: after a restart the files are back with their order of use;
// unfinished puts and foreign files are not served.
func TestReopen(t *testing.T) {
	dir := t.TempDir()
	c := newClock()
	// Room for the put in progress below without evicting a or b.
	s := open(t, dir, 3*4096, c)
	a, b := token(t), token(t)
	bodyA := data(t, 4096)
	put(t, s, a, bodyA)
	c.Add(time.Second)
	put(t, s, b, data(t, 4096))
	c.Add(time.Second)
	has(s, a) // b is now the least recently served
	if err := s.Maintain(); err != nil {
		t.Fatal(err)
	}
	// A put in progress at the crash, and stray files.
	w, err := s.Put(token(t), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data(t, 100)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filesDir, "zz-not-a-token"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	s2 := open(t, dir, 2*4096, c)
	if left, _ := os.ReadDir(filepath.Join(dir, tmpDir)); len(left) != 0 {
		t.Fatalf("%d unfinished files left after a restart", len(left))
	}
	if used, n := s2.Usage(); used != 2*4096 || n != 2 {
		t.Fatalf("usage after a restart: %d bytes, %d files", used, n)
	}
	got, _, _, err := s2.ReadAt(a, 0, 4096)
	if err != nil || !bytes.Equal(got, bodyA) {
		t.Fatalf("a after a restart: %v", err)
	}
	// The order survived: b goes first.
	put(t, s2, token(t), data(t, 4096))
	if has(s2, b) || !has(s2, a) {
		t.Fatal("the order of use was lost in the restart")
	}
	w.Abort()
}

// TestReopenSmallerCache: files over a lowered ceiling are dropped at start.
func TestReopenSmallerCache(t *testing.T) {
	dir := t.TempDir()
	c := newClock()
	s := open(t, dir, 4*4096, c)
	for i := 0; i < 4; i++ {
		put(t, s, token(t), data(t, 4096))
	}
	s2 := open(t, dir, 2*4096, c)
	if used, n := s2.Usage(); used != 2*4096 || n != 2 {
		t.Fatalf("usage under a lowered ceiling: %d bytes, %d files", used, n)
	}
}

// TestReadError: a file the disk fails to read is dropped and reported
// for a reupload.
func TestReadError(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, 1<<20, newClock())
	tok := token(t)
	put(t, s, tok, data(t, 8192))
	h := hex.EncodeToString(tok)
	if err := os.Truncate(filepath.Join(dir, filesDir, h[:2], h), 100); err != nil {
		t.Fatal(err)
	}
	_, _, readErr, err := s.ReadAt(tok, 4096, 4096)
	if !errors.Is(err, ErrNotFound) || !readErr {
		t.Fatalf("a cut file: %v, read error %v", err, readErr)
	}
	if used, n := s.Usage(); used != 0 || n != 0 {
		t.Fatalf("a broken file is still counted: %d bytes, %d files", used, n)
	}
	if _, err := os.Stat(filepath.Join(dir, filesDir, h[:2], h)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a broken file is still on the disk: %v", err)
	}
}

func TestDeleteAndList(t *testing.T) {
	s := open(t, t.TempDir(), 1<<20, newClock())
	a, b := token(t), token(t)
	put(t, s, a, data(t, 10))
	put(t, s, b, data(t, 20))
	put(t, s, b, data(t, 30)) // pushed again: replaced
	items := s.List()
	if len(items) != 2 {
		t.Fatalf("%d files listed", len(items))
	}
	sizes := map[string]int64{}
	for _, it := range items {
		sizes[string(it.Token)] = it.Size
	}
	if sizes[string(a)] != 10 || sizes[string(b)] != 30 {
		t.Fatalf("sizes %v", sizes)
	}
	if used, _ := s.Usage(); used != 40 {
		t.Fatalf("usage %d after a replaced file", used)
	}
	if !s.Delete(a) || s.Delete(a) {
		t.Fatal("Delete does not report what it removed")
	}
	if has(s, a) {
		t.Fatal("a deleted file is served")
	}
}

func TestEgress(t *testing.T) {
	dir := t.TempDir()
	c := newClock()
	e, err := OpenEgress(dir, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	e.Add(1000)
	e.Add(234)
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	e2, err := OpenEgress(dir, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	if m, n := e2.Month(); m != "2026-11" || n != 1234 {
		t.Fatalf("after a restart: %s %d", m, n)
	}
	c.Add(15 * 24 * time.Hour) // December
	if m, n := e2.Month(); m != "2026-12" || n != 0 {
		t.Fatalf("a new month: %s %d", m, n)
	}
	e2.Add(5)
	if err := e2.Flush(); err != nil {
		t.Fatal(err)
	}
	e3, err := OpenEgress(dir, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	if m, n := e3.Month(); m != "2026-12" || n != 5 {
		t.Fatalf("after a restart in the new month: %s %d", m, n)
	}
}

// TestPathless: errors of the disk lose the file name, which is the token.
func TestPathless(t *testing.T) {
	h := hex.EncodeToString(token(t))
	_, err := os.Open(filepath.Join(t.TempDir(), h))
	got := pathless(err)
	if bytes.Contains([]byte(got.Error()), []byte(h)) || !errors.Is(got, os.ErrNotExist) {
		t.Fatalf("pathless: %v", got)
	}
	err = os.Rename(filepath.Join(t.TempDir(), h), filepath.Join(t.TempDir(), h+"x"))
	if got := pathless(err); bytes.Contains([]byte(got.Error()), []byte(h)) {
		t.Fatalf("pathless of a rename: %v", got)
	}
}
