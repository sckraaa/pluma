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

// Package store keeps the cipher of files on a CDN node's disk (ADR 0065
// §5): one file per file_token under files/<hex[0:2]>/<hex>, written to
// tmp/ first and renamed in place once complete and synced, so a file is
// either whole or absent. The index is in memory and rebuilt by scanning
// at start (what is left in tmp/ is removed); the order of last use is
// kept in index.json, written once a minute, so that a restart does not
// forget which files are cold. Files are dropped least recently served
// first when a new one needs room under the ceiling, and when they have not
// been served for the idle time.
package store

import (
	"bytes"
	"cmp"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

var (
	// ErrNotFound: the node does not have the file.
	ErrNotFound = errors.New("store: no such file")
	// ErrTooLarge: the file is larger than the whole cache.
	ErrTooLarge = errors.New("store: the file is larger than the cache")
	// ErrDiskLow: storing the file would leave less free space than the
	// reserve.
	ErrDiskLow = errors.New("store: the disk is below its reserve")
	// ErrInvalid: a token that is not cdn.TokenSize bytes or a negative size.
	ErrInvalid = errors.New("store: invalid token or size")
	// ErrSize: more or fewer bytes were written than announced.
	ErrSize = errors.New("store: the bytes written do not match the size")
)

const (
	filesDir  = "files"
	tmpDir    = "tmp"
	indexFile = "index.json"
)

// Config is where and how much a Store keeps.
type Config struct {
	// Dir is the data directory (CDN_DATA_DIR).
	Dir string
	// CacheBytes is the ceiling of the files held (CDN_CACHE_BYTES).
	CacheBytes int64
	// IdleTTL drops a file not served for that long (CDN_IDLE_TTL).
	IdleTTL time.Duration
	// DiskReserve is the free space a new file must leave on the volume.
	DiskReserve int64
	// Now returns the time; time.Now when nil (tests set it).
	Now func() time.Time
	// DiskFree returns the free bytes of the volume of Dir; FreeSpace when
	// nil (tests set it).
	DiskFree func(dir string) (int64, error)
}

// entry is a file of the index.
type entry struct {
	token string // the raw token bytes
	size  int64
	// putAt is when the file was stored, lastUsed when it was last served
	// (unix seconds; lastUsed starts at putAt).
	putAt    int64
	lastUsed int64
	elem     *list.Element // in Store.lru, most recently used first
}

// Store is the files of a node. It is safe for concurrent use.
type Store struct {
	cfg Config

	mu      sync.Mutex
	files   map[string]*entry
	lru     *list.List
	used    int64 // bytes of the files in the index
	pending int64 // bytes reserved by Puts in progress
	dirty   bool  // the order changed since index.json was written
}

// Item is a file the node holds.
type Item struct {
	Token []byte
	Size  int64
}

// Open opens the data directory, removes unfinished files and rebuilds the
// index; files over the ceiling (lowered since they were stored) are dropped
// least recently served first.
func Open(cfg Config) (*Store, error) {
	if cfg.Dir == "" || cfg.CacheBytes <= 0 {
		return nil, errors.New("store: Dir and a positive CacheBytes are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.DiskFree == nil {
		cfg.DiskFree = FreeSpace
	}
	s := &Store{cfg: cfg, files: map[string]*entry{}, lru: list.New()}

	for _, d := range []string{cfg.Dir, filepath.Join(cfg.Dir, filesDir)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
	}
	// What a Put left half-written before a crash or a restart.
	if err := os.RemoveAll(filepath.Join(cfg.Dir, tmpDir)); err != nil {
		return nil, fmt.Errorf("store: clean %s: %w", tmpDir, err)
	}
	if err := os.Mkdir(filepath.Join(cfg.Dir, tmpDir), 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	order, err := s.readIndex()
	if err != nil {
		// The order is a hint: without it every file counts as used at its
		// modification time.
		logx.Errorf("cdn store: %s is unreadable, the order of use is rebuilt from the files: %v", indexFile, err)
		order = nil
	}
	if err := s.scan(order); err != nil {
		return nil, err
	}
	s.mu.Lock()
	for _, e := range s.evictLocked(0, cfg.CacheBytes) {
		s.removeLocked(e, metrics.EvictCapacity)
	}
	s.publishLocked()
	s.mu.Unlock()
	return s, nil
}

// indexRecord is a file of index.json.
type indexRecord struct {
	PutAt    int64 `json:"put_at"`
	LastUsed int64 `json:"last_used"`
}

type indexDoc struct {
	Version int                    `json:"version"`
	Files   map[string]indexRecord `json:"files"` // by hex token
}

func (s *Store) readIndex() (map[string]indexRecord, error) {
	b, err := os.ReadFile(filepath.Join(s.cfg.Dir, indexFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc indexDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("version %d", doc.Version)
	}
	return doc.Files, nil
}

// scan adds every complete file under files/ to the index.
func (s *Store) scan(order map[string]indexRecord) error {
	root := filepath.Join(s.cfg.Dir, filesDir)
	type found struct {
		e   *entry
		key int64
	}
	var all []found
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		token, err := hex.DecodeString(name)
		if err != nil || len(token) != cdn.TokenSize || filepath.Base(filepath.Dir(path)) != name[:2] {
			// Not a file of this store: left alone, not served.
			logx.Errorf("cdn store: skipping a foreign file under %s", filesDir)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := &entry{token: string(token), size: info.Size(), putAt: info.ModTime().Unix()}
		e.lastUsed = e.putAt
		if r, ok := order[name]; ok {
			e.putAt, e.lastUsed = r.PutAt, r.LastUsed
		}
		all = append(all, found{e: e, key: e.lastUsed})
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: scan: %w", pathless(err))
	}
	// Oldest first, each pushed to the front: the most recent ends first.
	slices.SortFunc(all, func(a, b found) int { return cmp.Compare(a.key, b.key) })
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range all {
		f.e.elem = s.lru.PushFront(f.e)
		s.files[f.e.token] = f.e
		s.used += f.e.size
	}
	return nil
}

func (s *Store) path(token string) string {
	h := hex.EncodeToString([]byte(token))
	return filepath.Join(s.cfg.Dir, filesDir, h[:2], h)
}

// publishLocked sets the gauges of the cache.
func (s *Store) publishLocked() {
	metrics.CacheBytes.Set(float64(s.used))
	metrics.CacheFiles.Set(float64(len(s.files)))
}

// evictLocked takes out of the index, least recently served first, the
// files that must go for need more bytes to fit under ceiling, and returns
// them (still on the disk: removeLocked).
func (s *Store) evictLocked(need, ceiling int64) []*entry {
	var out []*entry
	for s.used+s.pending+need > ceiling {
		back := s.lru.Back()
		if back == nil {
			break
		}
		e := back.Value.(*entry)
		s.dropLocked(e)
		out = append(out, e)
	}
	return out
}

func (s *Store) dropLocked(e *entry) {
	s.lru.Remove(e.elem)
	delete(s.files, e.token)
	s.used -= e.size
	s.dirty = true
}

// removeLocked deletes the file of an entry already out of the index. It
// runs under the lock, as Commit's rename does, so that it never deletes a
// file of the same token committed in between. A reader that opened the file
// before keeps reading it (the inode lives until closed).
func (s *Store) removeLocked(e *entry, reason string) {
	if err := os.Remove(s.path(e.token)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logx.Errorf("cdn store: remove a file (%s): %v", reason, pathless(err))
	}
	metrics.Evictions.WithLabelValues(reason).Inc()
	if reason == metrics.EvictCapacity {
		metrics.CapacityEvictionAge.Observe(float64(s.cfg.Now().Unix() - e.putAt))
	}
}

// ReadAt returns the bytes of the file token in [offset, offset+limit),
// cut at the end of the file, and the file's size; at or past the end it
// returns no bytes. A file the disk fails to read is dropped and reported
// as ErrNotFound (the client asks the home to push it again), with
// readErr set.
func (s *Store) ReadAt(token []byte, offset int64, limit int) (b []byte, size int64, readErr bool, err error) {
	if offset < 0 || limit < 0 {
		return nil, 0, false, ErrInvalid
	}
	s.mu.Lock()
	e, ok := s.files[string(token)]
	if !ok {
		s.mu.Unlock()
		return nil, 0, false, ErrNotFound
	}
	now := s.cfg.Now().Unix()
	if e.lastUsed != now {
		e.lastUsed = now
		s.dirty = true
	}
	s.lru.MoveToFront(e.elem)
	size = e.size
	s.mu.Unlock()

	if offset >= size {
		return nil, size, false, nil
	}
	n := int64(limit)
	if offset+n > size {
		n = size - offset
	}
	b = make([]byte, n)
	f, err := os.Open(s.path(e.token))
	if err == nil {
		_, err = f.ReadAt(b, offset)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		// Missing, cut short or unreadable: the file cannot be served.
		logx.Errorf("cdn store: read a file: %v", pathless(err))
		s.mu.Lock()
		if cur, ok := s.files[e.token]; ok && cur == e {
			s.dropLocked(e)
			s.removeLocked(e, metrics.EvictReadError)
			s.publishLocked()
		}
		s.mu.Unlock()
		return nil, 0, true, ErrNotFound
	}
	return b, size, false, nil
}

// Writer is a Put in progress.
type Writer struct {
	s       *Store
	token   string
	size    int64
	f       *os.File
	written int64
	sum     hash.Hash
	done    bool
}

// Put starts storing the file token of size bytes. Room is made at once:
// files least recently served are dropped until it fits under the ceiling
// with the other Puts in progress. A file of the same token is replaced
// when the new one is committed.
func (s *Store) Put(token []byte, size int64) (*Writer, error) {
	if len(token) != cdn.TokenSize || size < 0 {
		return nil, ErrInvalid
	}
	if size > s.cfg.CacheBytes {
		return nil, ErrTooLarge
	}
	free, err := s.cfg.DiskFree(s.cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("store: free space: %w", err)
	}
	s.mu.Lock()
	// Bytes the evictions below give back count as free.
	evicted := s.evictLocked(size, s.cfg.CacheBytes)
	var back int64
	for _, e := range evicted {
		back += e.size
	}
	if free+back-s.pending-size < s.cfg.DiskReserve {
		// Nothing is evicted for a file that does not fit on the disk
		// anyway: the entries go back in their places.
		for i := len(evicted) - 1; i >= 0; i-- {
			e := evicted[i]
			e.elem = s.lru.PushBack(e)
			s.files[e.token] = e
			s.used += e.size
		}
		s.mu.Unlock()
		return nil, ErrDiskLow
	}
	for _, e := range evicted {
		s.removeLocked(e, metrics.EvictCapacity)
	}
	s.pending += size
	s.publishLocked()
	s.mu.Unlock()

	f, err := os.CreateTemp(filepath.Join(s.cfg.Dir, tmpDir), "put-*")
	if err != nil {
		s.release(size)
		return nil, fmt.Errorf("store: %w", pathless(err))
	}
	return &Writer{s: s, token: string(token), size: size, f: f, sum: sha256.New()}, nil
}

func (s *Store) release(size int64) {
	s.mu.Lock()
	s.pending -= size
	s.mu.Unlock()
}

// Write appends the next bytes of the file.
func (w *Writer) Write(b []byte) (int, error) {
	if w.done {
		return 0, errors.New("store: write after the end of a put")
	}
	if w.written+int64(len(b)) > w.size {
		return 0, ErrSize
	}
	n, err := w.f.Write(b)
	w.written += int64(n)
	w.sum.Write(b[:n])
	if err != nil {
		return n, fmt.Errorf("store: %w", pathless(err))
	}
	return n, nil
}

// Commit syncs the file and puts it in place; it returns the SHA-256 of the
// bytes written. With fewer bytes than the size nothing is stored (ErrSize).
func (w *Writer) Commit() ([]byte, error) {
	if w.done {
		return nil, errors.New("store: put already finished")
	}
	if w.written != w.size {
		w.Abort()
		return nil, ErrSize
	}
	w.done = true
	s := w.s
	defer s.release(w.size)

	tmp := w.f.Name()
	err := w.f.Sync()
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	dst := s.path(w.token)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(dst), 0o700)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("store: commit: %w", pathless(err))
	}

	now := s.cfg.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Under the lock: an eviction of the same token removes its file under
	// it too (removeLocked).
	err = os.Rename(tmp, dst)
	if err == nil {
		err = syncDir(filepath.Dir(dst))
	}
	if err != nil {
		_ = os.Remove(tmp)
		// The rename may have happened before the sync failed: whatever is
		// at dst is not trusted, and an older file of the token goes too.
		_ = os.Remove(dst)
		if old, ok := s.files[w.token]; ok {
			s.dropLocked(old)
			s.publishLocked()
		}
		return nil, fmt.Errorf("store: commit: %w", pathless(err))
	}
	if old, ok := s.files[w.token]; ok {
		// The same token pushed again: the rename replaced its file.
		s.dropLocked(old)
	}
	e := &entry{token: w.token, size: w.size, putAt: now, lastUsed: now}
	e.elem = s.lru.PushFront(e)
	s.files[w.token] = e
	s.used += w.size
	s.dirty = true
	s.publishLocked()
	return w.sum.Sum(nil), nil
}

// Abort drops a Put: nothing of it stays on the disk. It does nothing after
// Commit.
func (w *Writer) Abort() {
	if w.done {
		return
	}
	w.done = true
	_ = w.f.Close()
	if err := os.Remove(w.f.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logx.Errorf("cdn store: remove an unfinished put: %v", pathless(err))
	}
	w.s.release(w.size)
}

// Delete removes the file token; it reports whether the node had it.
func (s *Store) Delete(token []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.files[string(token)]
	if ok {
		s.dropLocked(e)
		s.removeLocked(e, metrics.EvictDeleted)
		s.publishLocked()
	}
	return ok
}

// List returns every file held.
func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, 0, len(s.files))
	for el := s.lru.Front(); el != nil; el = el.Next() {
		e := el.Value.(*entry)
		out = append(out, Item{Token: []byte(e.token), Size: e.size})
	}
	return out
}

// Usage returns the bytes and the number of the files held.
func (s *Store) Usage() (used int64, files int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used, len(s.files)
}

// Cap returns the ceiling of the files held.
func (s *Store) Cap() int64 { return s.cfg.CacheBytes }

// Disk returns the free space of the volume and whether it is below the
// reserve.
func (s *Store) Disk() (free int64, low bool, err error) {
	free, err = s.cfg.DiskFree(s.cfg.Dir)
	if err != nil {
		return 0, true, err
	}
	metrics.DiskFree.Set(float64(free))
	return free, free < s.cfg.DiskReserve, nil
}

// Maintain drops the files not served for the idle time and writes
// index.json if the order changed. It runs once a minute and at shutdown.
func (s *Store) Maintain() error {
	now := s.cfg.Now()
	s.mu.Lock()
	if s.cfg.IdleTTL > 0 {
		limit := now.Add(-s.cfg.IdleTTL).Unix()
		for el := s.lru.Back(); el != nil; {
			e := el.Value.(*entry)
			if e.lastUsed > limit {
				break
			}
			el = el.Prev()
			s.dropLocked(e)
			s.removeLocked(e, metrics.EvictIdle)
		}
	}
	s.publishLocked()
	var doc *indexDoc
	if s.dirty {
		doc = &indexDoc{Version: 1, Files: make(map[string]indexRecord, len(s.files))}
		for _, e := range s.files {
			doc.Files[hex.EncodeToString([]byte(e.token))] = indexRecord{PutAt: e.putAt, LastUsed: e.lastUsed}
		}
		s.dirty = false
	}
	s.mu.Unlock()
	if doc == nil {
		return nil
	}
	if err := writeFileAtomic(filepath.Join(s.cfg.Dir, indexFile), doc); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return fmt.Errorf("store: write %s: %w", indexFile, err)
	}
	return nil
}

// writeFileAtomic writes v as JSON to path through a synced temporary file
// and a rename.
func writeFileAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = io.Copy(f, bytes.NewReader(b))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir makes a rename in dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// pathless drops the file names from err: they hold file tokens, which
// never go to a log (task 102, "Лог и приватность"). The cause stays for
// errors.Is.
func pathless(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return fmt.Errorf("%s: %w", le.Op, le.Err)
	}
	return err
}
