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
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// ErrRequestTokenInvalid is the answer of upload.reuploadCdnFile to a
// request_token the node did not sign, or signed too long ago
// (core.telegram.org/method/upload.reuploadCdnFile): the clients go back to
// upload.getFile (Android FileLoadOperation.java:2645-2649).
var ErrRequestTokenInvalid = status.Error(mtproto.ErrBadRequest, "REQUEST_TOKEN_INVALID")

// ErrNoFile is what a Sizer returns for a location that names no file.
var ErrNoFile = errors.New("cdn: no such file")

// Sizer finds the size of a file the CDN may serve: media for documents
// and photos, the secret chats' table for their files.
type Sizer interface {
	// FileSize returns the size in bytes of the file of loc, a location Key
	// accepts; ErrNoFile if there is none.
	FileSize(ctx context.Context, loc *mtproto.InputFileLocation) (int64, error)
}

// Limits of the home side (task 102).
const (
	// stateCacheBytes and stateCacheTTL bound the cache of the files'
	// states upload.getFile decides by: a client asks for a file a
	// megabyte at a time, and MySQL is not asked every time.
	stateCacheBytes = 64 << 20
	stateCacheTTL   = time.Minute
	// nodeCacheTTL: the row of the node is read again after it; the
	// prober renews healthy_until every 10 s for 30 s.
	nodeCacheTTL = 2 * time.Second
	// reuploadsPerMinute is how many upload.reuploadCdnFile a user may call
	// a minute; then FLOOD_WAIT_X.
	reuploadsPerMinute = 30
	// reuploadPoll is how often upload.reuploadCdnFile looks whether the
	// copy is ready.
	reuploadPoll = 250 * time.Millisecond
)

// Home is the side of the CDN the methods of the clients use: the decision
// of upload.getFile, upload.getCdnFileHashes, upload.reuploadCdnFile and
// help.getCdnConfig (ADR 0065 §2-4). It is safe for concurrent use.
type Home struct {
	store Store
	set   *Settings
	sizer Sizer
	now   func() time.Time
	poll  time.Duration

	files *stateCache

	nodeMu sync.Mutex
	node   *Node // nil: none read yet, or no row
	nodeAt time.Time

	reuploads *userLimiter
	// wake tells the pusher a copy was queued; nil: it finds it by itself.
	wake func()
}

// NewHome returns the Home of the node of set over store.
func NewHome(store Store, set *Settings, sizer Sizer) *Home {
	return &Home{
		store:     store,
		set:       set,
		sizer:     sizer,
		now:       time.Now,
		poll:      reuploadPoll,
		files:     newStateCache(stateCacheBytes, stateCacheTTL),
		reuploads: newUserLimiter(reuploadsPerMinute, time.Minute),
	}
}

// DcId is the CDN DC of the node.
func (h *Home) DcId() int32 { return h.set.DcId }

// fileState is what upload.getFile decides a file by.
type fileState struct {
	file  *File
	ready bool // a whole checked copy is on the node
}

// Redirect decides a request of upload.getFile that Wants a redirect, after
// the checks of the request: upload.fileCdnRedirect when a ready copy of
// the file is on a healthy node, nil when the file is served from home. A
// file that may go through the CDN and has no copy is queued for a push.
// The result is for the metrics and the tests, empty for a location that
// never goes through the CDN. The redirect is an optimization: a failure
// here is logged and the file served from home.
func (h *Home) Redirect(ctx context.Context, loc *mtproto.InputFileLocation, offset int64) (*mtproto.Upload_File, string) {
	key, ok := Key(loc)
	if !ok {
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
	node, err := h.nodeRow(ctx)
	if err != nil {
		return nil, ResultError, err
	}
	if !h.healthy(node) {
		return nil, ResultUnhealthy, nil
	}
	if !st.ready {
		return nil, ResultNotReady, nil
	}
	hashes, err := Answer(st.file.Hashes, st.file.Size, offset)
	if err != nil {
		return nil, ResultError, err
	}
	return mtproto.MakeTLUploadFileCdnRedirect(&mtproto.Upload_File{
		DcId:          h.set.DcId,
		FileToken:     st.file.Secrets.Token,
		EncryptionKey: st.file.Secrets.Key,
		EncryptionIv:  st.file.Secrets.IV,
		FileHashes:    hashes,
	}).To_Upload_File(), ResultRedirect, nil
}

// state returns the state of the file of key from the cache, or reads it,
// making its row and queueing its push as needed.
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
		r, err := h.store.Replica(ctx, f.RowId, h.set.DcId)
		if err != nil {
			return nil, err
		}
		st.ready = r != nil && r.State == StateReady && f.HasKey() && f.Hashes != nil
		if !st.ready {
			// Idempotent: a queued or pushing copy stays as it is, an
			// absent one is queued anew, a failed one is left to the
			// operator.
			if err = h.store.Enqueue(ctx, f.RowId, h.set.DcId, 0, now.UnixMilli()); err != nil {
				return nil, err
			}
			h.wakePusher()
		}
	}
	h.files.put(key, st, now)
	return st, nil
}

func (h *Home) wakePusher() {
	if h.wake != nil {
		h.wake()
	}
}

// Forget drops the cached state of the file of key: its copy changed.
func (h *Home) Forget(key FileKey) { h.files.remove(key) }

// nodeRow returns the row of the node, read at most nodeCacheTTL ago; nil
// if it has none yet.
func (h *Home) nodeRow(ctx context.Context) (*Node, error) {
	now := h.now()
	h.nodeMu.Lock()
	if !h.nodeAt.IsZero() && now.Sub(h.nodeAt) < nodeCacheTTL && now.After(h.nodeAt) {
		n := h.node
		h.nodeMu.Unlock()
		return n, nil
	}
	h.nodeMu.Unlock()
	n, err := h.store.Node(ctx, h.set.DcId)
	if err != nil {
		return nil, err
	}
	h.nodeMu.Lock()
	h.node, h.nodeAt = n, now
	h.nodeMu.Unlock()
	return n, nil
}

// healthy reports whether n takes redirects: the prober saw it well less
// than 30 s ago, and its key is known to give out in help.getCdnConfig.
func (h *Home) healthy(n *Node) bool {
	return n != nil && n.PublicKey != "" && n.HealthyUntil > h.now().UnixMilli()
}

// FileHashes answers upload.getCdnFileHashes: the hashes of the file of a
// file_token from offset. FILE_TOKEN_INVALID sends the client back to
// upload.getFile (core.telegram.org/cdn) for a token that is not ours, a
// file whose copy was never ready, and a failure here.
func (h *Home) FileHashes(ctx context.Context, token []byte, offset int64) ([]*mtproto.FileHash, error) {
	if len(token) != TokenSize || offset < 0 {
		return nil, mtproto.ErrFileTokenInvalid
	}
	f, err := h.store.ByToken(ctx, token)
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

// Reupload answers upload.reuploadCdnFile of user (ADR 0065 §4): the node
// said with requestToken that it does not have the file of fileToken. The
// copy is made absent and pushed first, and the hashes from offset 0 are
// the answer once it is ready, within ReuploadWait; the client then asks
// the node again (Android FileLoadOperation.java:2623-2644). Any other
// outcome sends the client back to upload.getFile: FILE_TOKEN_INVALID, or
// REQUEST_TOKEN_INVALID for a token the node did not sign (Android
// :2645-2649, tdesktop download_manager_mtproto.cpp:958-980). A failure
// here is logged and answered FILE_TOKEN_INVALID.
func (h *Home) Reupload(ctx context.Context, userId int64, fileToken, requestToken []byte) ([]*mtproto.FileHash, error) {
	hashes, result, err := h.reupload(ctx, userId, fileToken, requestToken)
	metricReuploads.Inc(result)
	var fail *reuploadFailure
	if errors.As(err, &fail) {
		logx.WithContext(ctx).Errorf("cdn: upload.reuploadCdnFile: %v", fail.err)
		return nil, mtproto.ErrFileTokenInvalid
	}
	return hashes, err
}

// reuploadFailure is a failure of reupload, answered FILE_TOKEN_INVALID.
type reuploadFailure struct{ err error }

func (f *reuploadFailure) Error() string { return f.err.Error() }

func reuploadFailed(err error) error { return &reuploadFailure{err: err} }

func (h *Home) reupload(ctx context.Context, userId int64, fileToken, requestToken []byte) ([]*mtproto.FileHash, string, error) {
	now := h.now()
	if wait := h.reuploads.take(userId, now); wait > 0 {
		return nil, reuploadFlood, mtproto.NewErrFloodWaitX(int32((wait + time.Second - 1) / time.Second))
	}
	if len(fileToken) != TokenSize {
		return nil, reuploadTokenInvalid, mtproto.ErrFileTokenInvalid
	}
	f, err := h.store.ByToken(ctx, fileToken)
	if err != nil {
		return nil, reuploadError, reuploadFailed(err)
	}
	if f == nil {
		return nil, reuploadTokenInvalid, mtproto.ErrFileTokenInvalid
	}
	// The keys are read afresh: the cached row may be older than a
	// rotation.
	node, err := h.store.Node(ctx, h.set.DcId)
	if err != nil {
		return nil, reuploadError, reuploadFailed(err)
	}
	if node == nil || !VerifyRequestToken(node.RtKeys(), fileToken, requestToken, now) {
		return nil, reuploadRequestTokenWrong, ErrRequestTokenInvalid
	}
	if !h.healthy(node) {
		// No copy is pushed to a node that is out; the client is served
		// from home.
		return nil, reuploadUnhealthy, mtproto.ErrFileTokenInvalid
	}

	ms := now.UnixMilli()
	if _, err = h.store.MarkAbsent(ctx, f.RowId, h.set.DcId, ms); err != nil {
		return nil, reuploadError, reuploadFailed(err)
	}
	if err = h.store.Enqueue(ctx, f.RowId, h.set.DcId, PriorityReupload, ms); err != nil {
		return nil, reuploadError, reuploadFailed(err)
	}
	h.Forget(f.Key)
	h.wakePusher()

	ctx, cancel := context.WithTimeout(ctx, h.set.ReuploadWait)
	defer cancel()
	tick := time.NewTicker(h.poll)
	defer tick.Stop()
	for {
		r, err := h.store.Replica(ctx, f.RowId, h.set.DcId)
		switch {
		case ctx.Err() != nil:
			return nil, reuploadTimeout, mtproto.ErrFileTokenInvalid
		case err != nil:
			return nil, reuploadError, reuploadFailed(err)
		case r != nil && r.State == StateReady:
			ready, err := h.store.ByToken(ctx, fileToken)
			if err != nil {
				return nil, reuploadError, reuploadFailed(err)
			}
			if ready == nil || ready.Hashes == nil {
				return nil, reuploadTokenInvalid, mtproto.ErrFileTokenInvalid
			}
			hashes, err := Answer(ready.Hashes, ready.Size, 0)
			if err != nil {
				return nil, reuploadError, reuploadFailed(err)
			}
			return hashes, reuploadOK, nil
		case r == nil || r.State == StateFailed || r.State == StateAbsent:
			// Failed for good, or made absent again by the inventory:
			// it will not be ready within the wait.
			return nil, reuploadTimeout, mtproto.ErrFileTokenInvalid
		}
		select {
		case <-ctx.Done():
			return nil, reuploadTimeout, mtproto.ErrFileTokenInvalid
		case <-tick.C:
		}
	}
}

// CdnConfig answers help.getCdnConfig: the keys of every node known, the
// healthy and the ones out of the redirects alike, since clients may still
// be downloading from those (task 102). Android and tdesktop ask for it
// when resPQ offers no key they know (Android Handshake.cpp:404-407,
// tdesktop session_private.cpp:1028-1031); the key is PKCS#1 PEM
// (Handshake.cpp:1032).
func (h *Home) CdnConfig(ctx context.Context) (*mtproto.CdnConfig, error) {
	nodes, err := h.store.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	return CdnConfigOf(nodes), nil
}

// CdnConfigOf is the cdnConfig of nodes; empty for none, as with the CDN
// off.
func CdnConfigOf(nodes []*Node) *mtproto.CdnConfig {
	keys := make([]*mtproto.CdnPublicKey, 0, len(nodes))
	for _, n := range nodes {
		if n.PublicKey == "" {
			continue
		}
		keys = append(keys, mtproto.MakeTLCdnPublicKey(&mtproto.CdnPublicKey{
			DcId:      n.DcId,
			PublicKey: n.PublicKey,
		}).To_CdnPublicKey())
	}
	return mtproto.MakeTLCdnConfig(&mtproto.CdnConfig{PublicKeys: keys}).To_CdnConfig()
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
