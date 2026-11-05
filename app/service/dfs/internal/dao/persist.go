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

package dao

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/contextx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"github.com/zeromicro/go-zero/core/rescue"

	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
)

// metricPersistFailed counts the stores that failed every attempt.
var metricPersistFailed = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "dfs",
	Name:      "store_failed_total",
	Help:      "Uploads not stored in object storage after every attempt.",
	Labels:    []string{},
})

// Background stores of uploads into object storage (task 057). An upload
// handler answers at once and copies the file afterwards; meanwhile it is
// downloaded from its parts (SetCacheFileInfo). The stores were unbounded
// goroutines without a deadline, and lost on a restart: now a few run at
// once, each within a deadline, retried; each is recorded next to its parts
// before it starts and resumed at the next start until it succeeds, and the
// sweep leaves such parts alone.
const (
	persistParallel = 4
	persistDeadline = 15 * time.Minute
	persistAttempts = 3
	persistBackoff  = 5 * time.Second
)

// StoreKind names the bucket a store goes to.
type StoreKind string

const (
	StoreDocument  StoreKind = "documents"
	StoreEncrypted StoreKind = "encrypted"
)

// pendingStore is the record of a store, next to the parts it reads.
type pendingStore struct {
	Kind    StoreKind `json:"kind"`
	Path    string    `json:"path"`
	CacheId int64     `json:"cache_id"`
	Owner   int64     `json:"owner"`
	FileId  int64     `json:"file_id"`
	Parts   int       `json:"parts"`
}

// putFunc stores size bytes of r at path in the bucket of kind.
type putFunc func(ctx context.Context, kind StoreKind, path string, r io.Reader, size int64) error

type persister struct {
	put    putFunc
	slots  chan struct{}
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newPersister(put putFunc) *persister {
	return &persister{put: put, slots: make(chan struct{}, persistParallel)}
}

// putObject is the putFunc of object storage.
func (d *Dao) putObject(ctx context.Context, kind StoreKind, path string, r io.Reader, size int64) error {
	var err error
	switch kind {
	case StoreDocument:
		_, err = d.PutDocumentFile(ctx, path, r, size)
	case StoreEncrypted:
		_, err = d.PutEncryptedFile(ctx, path, r, size)
	default:
		err = fmt.Errorf("dfs: unknown store kind %q", kind)
	}
	return err
}

// PersistParts stores the upload r at path in the bucket of kind, in the
// background. cacheId is the id downloads use meanwhile (SetCacheFileInfo):
// once stored, downloads go to object storage.
func (d *Dao) PersistParts(ctx context.Context, kind StoreKind, path string, cacheId int64, r *PartsReader) {
	p := pendingStore{Kind: kind, Path: path, CacheId: cacheId, Owner: r.Creator, FileId: r.FileId, Parts: r.FileTotalParts}
	if err := d.parts.markStore(p); err != nil {
		// Still stored now; only a restart in between would lose it.
		logx.WithContext(ctx).Errorf("dfs: record the store of %s: %v", path, err)
	}
	d.persist1(contextx.ValueOnlyFrom(ctx), p, r)
}

// persist1 runs the store p of r: at most persistParallel at once, each
// attempt within persistDeadline, up to persistAttempts. After shutdown has
// begun it is left recorded for the next start.
func (d *Dao) persist1(ctx context.Context, p pendingStore, r *PartsReader) {
	ps := d.persist
	ps.mu.Lock()
	if ps.closed {
		ps.mu.Unlock()
		logx.WithContext(ctx).Infof("dfs: stopping; the store of %s resumes at the next start", p.Path)
		return
	}
	ps.wg.Add(1)
	ps.mu.Unlock()
	go func() {
		defer ps.wg.Done()
		defer rescue.Recover()
		ps.slots <- struct{}{}
		defer func() { <-ps.slots }()
		for attempt := 1; ; attempt++ {
			actx, cancel := context.WithTimeout(ctx, persistDeadline)
			err := ps.put(actx, p.Kind, p.Path, io.NewSectionReader(r, 0, r.Size()), r.Size())
			cancel()
			if err == nil {
				break
			}
			if attempt == persistAttempts {
				// Recorded still: retried at the next start, while the
				// parts last (pendingTTL); downloads use the parts.
				logx.WithContext(ctx).Errorf("dfs: store %s failed after %d attempts: %v", p.Path, attempt, err)
				metricPersistFailed.Inc()
				return
			}
			logx.WithContext(ctx).Errorf("dfs: store %s, attempt %d: %v", p.Path, attempt, err)
			time.Sleep(persistBackoff * time.Duration(attempt))
		}
		d.stored(ctx, p)
	}()
}

// stored ends the store p: downloads go to object storage and the record
// goes. The parts stay for their time to live (partsTTL), as they stayed in
// Redis before: a client may send the same upload again meanwhile.
func (d *Dao) stored(ctx context.Context, p pendingStore) {
	if err := d.DelCacheFileInfo(ctx, p.CacheId); err != nil {
		logx.WithContext(ctx).Errorf("dfs: stored %s, but downloads still read its parts: %v", p.Path, err)
	}
	if err := d.parts.unmarkStore(p); err != nil {
		logx.WithContext(ctx).Errorf("dfs: stored %s, record: %v", p.Path, err)
	}
}

// ResumeStores restarts the stores recorded and not finished: cut by a
// restart, or failed every attempt before it.
func (d *Dao) ResumeStores(ctx context.Context) {
	pending, err := d.parts.pendingStores()
	if err != nil {
		logx.WithContext(ctx).Errorf("dfs: resume stores: %v", err)
	}
	for _, p := range pending {
		r, err := d.parts.openParts(&model.DfsFileInfo{Creator: p.Owner, FileId: p.FileId, FileTotalParts: p.Parts})
		if err != nil {
			logx.WithContext(ctx).Errorf("dfs: resume the store of %s: %v", p.Path, err)
			continue
		}
		logx.WithContext(ctx).Infof("dfs: resuming the store of %s", p.Path)
		d.persist1(ctx, p, r)
	}
}

// WaitPersisted stops new stores (they resume at the next start) and waits
// for those running, up to ctx.
func (d *Dao) WaitPersisted(ctx context.Context) {
	d.persist.mu.Lock()
	d.persist.closed = true
	d.persist.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.persist.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logx.Errorf("dfs: stopped with stores unfinished; they resume at the next start")
	}
}
