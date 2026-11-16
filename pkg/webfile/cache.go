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
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"golang.org/x/sync/singleflight"
)

// Defaults of the cache (WebFile in bff.yaml).
const (
	DefaultCacheDir   = "/tmp/webfile"
	DefaultCacheBytes = 2 << 30
	DefaultCacheTTL   = 24 * time.Hour
)

// fillTimeout bounds one fill of the cache: the source's own fetch is
// shorter (10 s for an address); waiting for a slot counts too.
const fillTimeout = 30 * time.Second

// maxFills is how many files are fetched at once by the whole process: a
// fill holds its file in memory (up to 20 MB for an address) until it is
// on disk.
const maxFills = 4

// cacheFile names the files of the cache in its directory, and only them:
// a start removes what the last run left, and nothing else.
var cacheFile = regexp.MustCompile(`^([0-9a-f]{32}-[0-9]+\.wf|fill-[0-9]+\.tmp)$`)

var metricCacheBytes = metric.NewGaugeVec(&metric.GaugeVecOpts{
	Namespace: "webfile",
	Name:      "cache_bytes",
	Help:      "Bytes of the web files kept on disk.",
	Labels:    []string{},
})

// DiskCache keeps whole files on disk by key: at most maxBytes in all, the
// least recently used going first, each for at most ttl, and one fill of a
// key at a time. It keeps its index in memory: a start removes the files a
// previous run left, and they are fetched again when asked for.
type DiskCache struct {
	dir      string
	maxBytes int64
	ttl      time.Duration
	now      func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
	lru     *list.List // of *entry, the most recently used first
	total   int64
	seq     uint64

	group singleflight.Group
	fills chan struct{}
}

type entry struct {
	key      string
	path     string
	size     int64
	mimeType string
	mtime    int32
	stored   time.Time
	elem     *list.Element
	refs     int  // open Handles
	gone     bool // out of the index: the file goes with the last Handle
}

// NewDiskCache returns the cache in dir, which it creates, after removing
// the files of the cache a previous run left there.
func NewDiskCache(dir string, maxBytes int64, ttl time.Duration) (*DiskCache, error) {
	if dir == "" || maxBytes <= 0 || ttl <= 0 {
		return nil, fmt.Errorf("webfile: cache %q of %d bytes for %v", dir, maxBytes, ttl)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("webfile: cache: %w", err)
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("webfile: cache: %w", err)
	}
	for _, n := range names {
		if n.Type().IsRegular() && cacheFile.MatchString(n.Name()) {
			if err := os.Remove(filepath.Join(dir, n.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("webfile: cache: %w", err)
			}
		}
	}
	metricCacheBytes.Set(0)
	return &DiskCache{
		dir:      dir,
		maxBytes: maxBytes,
		ttl:      ttl,
		now:      time.Now,
		entries:  map[string]*entry{},
		lru:      list.New(),
		fills:    make(chan struct{}, maxFills),
	}, nil
}

// Handle is an open file of the cache; the caller closes it.
type Handle struct {
	c     *DiskCache
	e     *entry
	bytes []byte // a file larger than the cache, kept by no one
}

// Size is the size of the file.
func (h *Handle) Size() int64 {
	if h.e == nil {
		return int64(len(h.bytes))
	}
	return h.e.size
}

// MimeType is the type of the file.
func (h *Handle) MimeType() string { return h.e.mimeType }

// Mtime is the modification time of the file, unix seconds.
func (h *Handle) Mtime() int32 { return h.e.mtime }

// Read returns the bytes of [offset, offset+limit) of the file: none past
// its end.
func (h *Handle) Read(offset, limit int64) ([]byte, error) {
	size := h.Size()
	if offset >= size {
		return []byte{}, nil
	}
	n := min(limit, size-offset)
	if h.bytes != nil {
		return h.bytes[offset : offset+n], nil
	}
	f, err := os.Open(h.e.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, n)
	if _, err := f.ReadAt(b, offset); err != nil {
		return nil, err
	}
	return b, nil
}

// Close releases the file: an evicted file is removed with its last
// Handle.
func (h *Handle) Close() {
	if h.bytes != nil || h.c == nil {
		return
	}
	h.c.mu.Lock()
	defer h.c.mu.Unlock()
	h.e.refs--
	if h.e.gone && h.e.refs == 0 {
		h.c.removeFile(h.e)
	}
	h.c = nil
}

// fillResult is what a fill gives every caller waiting for it: the entry
// in the index, or the bytes of a file too large to keep.
type fillResult struct {
	e    *entry
	file *File
}

// Get returns the file of key: the kept copy while it is fresh, otherwise
// the one fill makes, once for every caller asking meanwhile. A fill goes
// on when the caller that started it stops waiting, for the others and the
// next ask; a caller waits as long as ctx allows.
func (c *DiskCache) Get(ctx context.Context, key string, fill func(ctx context.Context) (File, error)) (*Handle, error) {
	// A file evicted between its fill and the caller taking it is filled
	// again, once.
	for range 2 {
		if h := c.open(key); h != nil {
			return h, nil
		}
		ch := c.group.DoChan(key, func() (any, error) {
			return c.fill(context.WithoutCancel(ctx), key, fill)
		})
		var res singleflight.Result
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res = <-ch:
		}
		if res.Err != nil {
			return nil, res.Err
		}
		r := res.Val.(fillResult)
		if r.file != nil {
			return &Handle{e: &entry{size: int64(len(r.file.Bytes)), mimeType: r.file.MimeType, mtime: r.file.Mtime}, bytes: r.file.Bytes}, nil
		}
		if h := c.acquire(r.e); h != nil {
			return h, nil
		}
	}
	return nil, errors.New("webfile: cache: the file was evicted as soon as it was filled")
}

// open returns a Handle of the fresh copy of key, nil without one.
func (c *DiskCache) open(key string) *Handle {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	if c.now().Sub(e.stored) >= c.ttl {
		c.removeLocked(e)
		return nil
	}
	c.lru.MoveToFront(e.elem)
	e.refs++
	return &Handle{c: c, e: e}
}

// acquire returns a Handle of e if it is still kept.
func (c *DiskCache) acquire(e *entry) *Handle {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.gone {
		return nil
	}
	e.refs++
	return &Handle{c: c, e: e}
}

// fill gets the file of key and keeps it.
func (c *DiskCache) fill(ctx context.Context, key string, fill func(ctx context.Context) (File, error)) (fillResult, error) {
	ctx, cancel := context.WithTimeout(ctx, fillTimeout)
	defer cancel()
	select {
	case c.fills <- struct{}{}:
	case <-ctx.Done():
		return fillResult{}, ctx.Err()
	}
	defer func() { <-c.fills }()
	f, err := fill(ctx)
	if err != nil {
		return fillResult{}, err
	}
	size := int64(len(f.Bytes))
	if size > c.maxBytes {
		return fillResult{file: &f}, nil
	}
	c.mu.Lock()
	c.seq++
	seq := c.seq
	c.mu.Unlock()
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(c.dir, hex.EncodeToString(sum[:16])+"-"+strconv.FormatUint(seq, 10)+".wf")
	if err := writeFile(c.dir, seq, path, f.Bytes); err != nil {
		return fillResult{}, err
	}
	e := &entry{key: key, path: path, size: size, mimeType: f.MimeType, mtime: f.Mtime, stored: c.now()}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.removeLocked(old)
	}
	e.elem = c.lru.PushFront(e)
	c.entries[key] = e
	c.total += size
	c.evictLocked(e)
	metricCacheBytes.Set(float64(c.total))
	return fillResult{e: e}, nil
}

// writeFile writes b to path through a temporary file, so that no reader
// sees a part of it.
func writeFile(dir string, seq uint64, path string, b []byte) error {
	tmp := filepath.Join(dir, "fill-"+strconv.FormatUint(seq, 10)+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("webfile: cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("webfile: cache: %w", err)
	}
	return nil
}

// evictLocked drops the expired files, then the least recently used ones
// until the cache is within its size; keep stays.
func (c *DiskCache) evictLocked(keep *entry) {
	now := c.now()
	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		if e := el.Value.(*entry); e != keep && now.Sub(e.stored) >= c.ttl {
			c.removeLocked(e)
		}
		el = prev
	}
	for el := c.lru.Back(); el != nil && c.total > c.maxBytes; {
		prev := el.Prev()
		if e := el.Value.(*entry); e != keep {
			c.removeLocked(e)
		}
		el = prev
	}
}

// removeLocked takes e out of the index; its file goes now or with its
// last Handle.
func (c *DiskCache) removeLocked(e *entry) {
	if e.gone {
		return
	}
	e.gone = true
	delete(c.entries, e.key)
	c.lru.Remove(e.elem)
	c.total -= e.size
	metricCacheBytes.Set(float64(c.total))
	if e.refs == 0 {
		c.removeFile(e)
	}
}

func (c *DiskCache) removeFile(e *entry) {
	if err := os.Remove(e.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logx.Errorf("webfile: cache: a file not removed: %v", err)
	}
}

// Bytes is the size of the files kept.
func (c *DiskCache) Bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}
