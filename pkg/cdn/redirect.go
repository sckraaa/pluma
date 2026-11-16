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
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
)

// ErrNoFile is what a Sizer returns for a location that names no file.
var ErrNoFile = errors.New("cdn: no such file")

// Sizer finds the size of a file the CDN may serve: media for documents
// and photos.
type Sizer interface {
	// FileSize returns the size in bytes of the file of loc, a location Key
	// accepts; ErrNoFile if there is none.
	FileSize(ctx context.Context, loc *mtproto.InputFileLocation) (int64, error)
}

// Limits of the home side.
const (
	// stateCacheBytes and stateCacheTTL bound the cache of the files'
	// states upload.getFile decides by: a client asks for a file a
	// megabyte at a time, and MySQL is not asked every time.
	stateCacheBytes = 64 << 20
	stateCacheTTL   = time.Minute
	// healthCacheTTL: the health of the delivery is read again after it.
	healthCacheTTL = 2 * time.Second
	// reuploadsPerMinute is how many upload.reuploadCdnFile a user may call
	// a minute; then FLOOD_WAIT_X.
	reuploadsPerMinute = 30
)

// Home is the side of the CDN the methods of the clients use: the decision
// of upload.getFile, upload.getCdnFileHashes, upload.reuploadCdnFile and
// help.getCdnConfig (ADR 0071 §2, §9). It is safe for concurrent use.
type Home struct {
	store Store
	set   *Settings
	sizer Sizer
	now   func() time.Time
	// delivery is where files are redirected: the first of the route of
	// every client.
	delivery *Delivery

	files *stateCache

	healthMu sync.Mutex
	health   *DeliveryHealth // nil: none read yet, or no row
	healthAt time.Time

	reuploads *userLimiter
	// wake tells the preparer a file was queued; nil: it finds it by itself.
	wake func()
}

// NewHome returns the Home of set over store. set is a parsed Config:
// its route names an enabled delivery.
func NewHome(store Store, set *Settings, sizer Sizer) (*Home, error) {
	if len(set.Routes) == 0 || len(set.Routes[0].Deliveries) == 0 {
		return nil, errors.New("cdn: no route")
	}
	d, ok := set.Delivery(set.Routes[0].Deliveries[0])
	if !ok {
		return nil, fmt.Errorf("cdn: the route names delivery %d, which is off", set.Routes[0].Deliveries[0])
	}
	return &Home{
		store:     store,
		set:       set,
		sizer:     sizer,
		now:       time.Now,
		delivery:  d,
		files:     newStateCache(stateCacheBytes, stateCacheTTL),
		reuploads: newUserLimiter(reuploadsPerMinute, time.Minute),
	}, nil
}

// fileState is what upload.getFile decides a file by.
type fileState struct {
	file  *File
	ready bool // prepared for the origin: its hashes are stored
}

// Redirect decides a request of upload.getFile that Wants a redirect, after
// the checks of the request: upload.fileCdnRedirect with a PLH1 file_token
// (token.go) when the file is prepared and the delivery healthy, nil when
// the file is served from home. A file that may go through the CDN and is
// not prepared is queued. The result is for the metrics and the tests,
// empty for a location that never goes through the CDN. The redirect is an
// optimization: a failure here is logged and the file served from home.
func (h *Home) Redirect(ctx context.Context, loc *mtproto.InputFileLocation, offset int64) (*mtproto.Upload_File, string) {
	key, ok := Key(loc)
	if !ok || !h.set.AllowsKind(key.Kind) {
		return nil, ""
	}
	res, result, err := h.redirect(ctx, key, loc, offset)
	if err != nil {
		logx.WithContext(ctx).Errorf("cdn: upload.getFile served from home: %v", err)
	}
	metricRedirects.Inc(result)
	return res, result
}

func (h *Home) redirect(ctx context.Context, key FileKey, loc *mtproto.InputFileLocation, offset int64) (*mtproto.Upload_File, string, error) {
	st, err := h.state(ctx, key, loc)
	if err != nil {
		return nil, ResultError, err
	}
	if !st.file.Eligible {
		return nil, ResultSmall, nil
	}
	healthy, err := h.healthy(ctx)
	if err != nil {
		return nil, ResultError, err
	}
	if !healthy {
		return nil, ResultUnhealthy, nil
	}
	if !st.ready {
		return nil, ResultNotReady, nil
	}
	hashes, err := Answer(st.file.Hashes, st.file.Size, offset)
	if err != nil {
		return nil, ResultError, err
	}
	url, exp, err := h.delivery.Signer.SignURL(ObjectPath(st.file.Secrets.Token), h.now())
	if err != nil {
		return nil, ResultError, err
	}
	token, err := EncodeRedirectToken(st.file.Secrets.Token, exp, url)
	if err != nil {
		return nil, ResultError, err
	}
	return mtproto.MakeTLUploadFileCdnRedirect(&mtproto.Upload_File{
		DcId:          h.delivery.ID,
		FileToken:     token,
		EncryptionKey: st.file.Secrets.Key,
		EncryptionIv:  st.file.Secrets.IV,
		FileHashes:    hashes,
	}).To_Upload_File(), ResultRedirect, nil
}

// state returns the state of the file of key from the cache, or reads it,
// making its row and queueing its preparation as needed.
func (h *Home) state(ctx context.Context, key FileKey, loc *mtproto.InputFileLocation) (*fileState, error) {
	now := h.now()
	if st := h.files.get(key, now); st != nil {
		return st, nil
	}
	f, err := h.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if f == nil {
		size, err := h.sizer.FileSize(ctx, loc)
		if err != nil {
			return nil, err
		}
		if size <= 0 {
			return nil, ErrNoFile
		}
		if f, err = h.store.Ensure(ctx, &File{
			Key:       key,
			Size:      size,
			Eligible:  Eligible(size, h.set.MinFileSize) && size <= MaxCipherSize,
			CreatedAt: now.UnixMilli(),
		}); err != nil {
			return nil, err
		}
	}
	st := &fileState{file: f}
	if f.Eligible {
		target := h.delivery.Target()
		r, err := h.store.Replica(ctx, f.RowId, target)
		if err != nil {
			return nil, err
		}
		st.ready = r != nil && r.State == StateReady && f.HasKey() && f.Hashes != nil
		if !st.ready {
			// Idempotent: a queued or claimed preparation stays as it is,
			// an absent one is queued anew, a failed one is left to the
			// operator.
			if err = h.store.Enqueue(ctx, f.RowId, target, 0, now.UnixMilli()); err != nil {
				return nil, err
			}
			if h.wake != nil {
				h.wake()
			}
		}
	}
	h.files.put(key, st, now)
	return st, nil
}

// Forget drops the cached state of the file of key: its preparation
// changed.
func (h *Home) Forget(key FileKey) { h.files.remove(key) }

// healthy reports whether the delivery takes redirects: its last check,
// read at most healthCacheTTL ago, holds now.
func (h *Home) healthy(ctx context.Context) (bool, error) {
	now := h.now()
	h.healthMu.Lock()
	cached := !h.healthAt.IsZero() && now.Sub(h.healthAt) < healthCacheTTL && now.After(h.healthAt)
	d := h.health
	h.healthMu.Unlock()
	if !cached {
		var err error
		if d, err = h.store.DeliveryHealth(ctx, h.delivery.ID); err != nil {
			return false, err
		}
		h.healthMu.Lock()
		h.health, h.healthAt = d, now
		h.healthMu.Unlock()
	}
	return d != nil && d.HealthyUntil > now.UnixMilli(), nil
}

// FileHashes answers upload.getCdnFileHashes: the hashes of the file of a
// PLH1 file_token (token.go) from offset. FILE_TOKEN_INVALID sends the
// client back to upload.getFile (core.telegram.org/cdn) for a token that
// is not ours, a file that was never prepared, and a failure here.
func (h *Home) FileHashes(ctx context.Context, token []byte, offset int64) ([]*mtproto.FileHash, error) {
	id, _, _, err := ParseRedirectToken(token)
	if err != nil || offset < 0 {
		return nil, mtproto.ErrFileTokenInvalid
	}
	f, err := h.store.ByToken(ctx, id)
	if err != nil {
		logx.WithContext(ctx).Errorf("cdn: upload.getCdnFileHashes: %v", err)
		return nil, mtproto.ErrFileTokenInvalid
	}
	if f == nil || f.Hashes == nil {
		return nil, mtproto.ErrFileTokenInvalid
	}
	hashes, err := Answer(f.Hashes, f.Size, offset)
	if err != nil {
		logx.WithContext(ctx).Errorf("cdn: upload.getCdnFileHashes: file row %d: %v", f.RowId, err)
		return nil, mtproto.ErrFileTokenInvalid
	}
	return hashes, nil
}

// Reupload answers upload.reuploadCdnFile of user: always
// FILE_TOKEN_INVALID, past reuploadsPerMinute FLOOD_WAIT_X. An HTTPS
// delivery pulls a file it lacks from the origin by itself, and the patched
// clients never call the method (ADR 0071 §9); an unpatched one that does
// goes back to upload.getFile (Android FileLoadOperation.java:2645-2649,
// tdesktop download_manager_mtproto.cpp:958-980).
func (h *Home) Reupload(userId int64) error {
	if wait := h.reuploads.take(userId, h.now()); wait > 0 {
		metricReuploads.Inc(reuploadFlood)
		return mtproto.NewErrFloodWaitX(int32((wait + time.Second - 1) / time.Second))
	}
	metricReuploads.Inc(reuploadTokenInvalid)
	return mtproto.ErrFileTokenInvalid
}

// EmptyCdnConfig is the answer of help.getCdnConfig: no MTProto CDN DC
// has keys, the deliveries are HTTPS (ADR 0071 §9).
func EmptyCdnConfig() *mtproto.CdnConfig {
	return mtproto.MakeTLCdnConfig(&mtproto.CdnConfig{PublicKeys: []*mtproto.CdnPublicKey{}}).To_CdnConfig()
}

// stateCache is an LRU of the states of files bounded by the bytes they
// take, each kept for ttl.
type stateCache struct {
	mu       sync.Mutex
	maxBytes int
	ttl      time.Duration
	bytes    int
	order    *list.List // of *stateEntry, the most recent first
	byKey    map[FileKey]*list.Element
}

type stateEntry struct {
	key   FileKey
	st    *fileState
	at    time.Time
	bytes int
}

// stateOverhead is about what an entry takes besides its hashes.
const stateOverhead = 256

func newStateCache(maxBytes int, ttl time.Duration) *stateCache {
	return &stateCache{maxBytes: maxBytes, ttl: ttl, order: list.New(), byKey: map[FileKey]*list.Element{}}
}

func (c *stateCache) get(key FileKey, now time.Time) *fileState {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.byKey[key]
	if !ok {
		return nil
	}
	e := el.Value.(*stateEntry)
	if age := now.Sub(e.at); age >= c.ttl || age < 0 {
		c.drop(el)
		return nil
	}
	c.order.MoveToFront(el)
	return e.st
}

func (c *stateCache) put(key FileKey, st *fileState, now time.Time) {
	n := stateOverhead + len(st.file.Hashes)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.drop(el)
	}
	if n > c.maxBytes {
		return
	}
	c.byKey[key] = c.order.PushFront(&stateEntry{key: key, st: st, at: now, bytes: n})
	c.bytes += n
	for c.bytes > c.maxBytes {
		c.drop(c.order.Back())
	}
}

func (c *stateCache) remove(key FileKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.drop(el)
	}
}

// drop removes el; c.mu is held.
func (c *stateCache) drop(el *list.Element) {
	e := el.Value.(*stateEntry)
	c.order.Remove(el)
	delete(c.byKey, e.key)
	c.bytes -= e.bytes
}

// userLimiter counts the calls of each user in windows of a fixed length.
type userLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	users  map[int64]*userWindow
}

type userWindow struct {
	start time.Time
	n     int
}

// limiterSweepAt: past this many users the windows that ended are dropped.
const limiterSweepAt = 1024

func newUserLimiter(limit int, window time.Duration) *userLimiter {
	return &userLimiter{limit: limit, window: window, users: map[int64]*userWindow{}}
}

// take counts a call of user at now; it returns 0 if the call is within
// the limit, or how long until the window ends.
func (l *userLimiter) take(user int64, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.users[user]
	if !ok || now.Sub(w.start) >= l.window || now.Before(w.start) {
		if len(l.users) >= limiterSweepAt {
			for u, o := range l.users {
				if now.Sub(o.start) >= l.window || now.Before(o.start) {
					delete(l.users, u)
				}
			}
		}
		l.users[user] = &userWindow{start: now, n: 1}
		return 0
	}
	if w.n >= l.limit {
		return w.start.Add(l.window).Sub(now)
	}
	w.n++
	return 0
}
