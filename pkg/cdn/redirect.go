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
	"slices"
	"strconv"
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

// Caps tells the version of pluma_cdn (clientcaps.go) the client of a
// permanent auth key sent in initConnection.params, as the session stored
// them for the key.
type Caps interface {
	PlumaCdn(ctx context.Context, permAuthKeyID int64) (int, error)
}

// Locator tells the country of a client's address as
// authsession.getIpCountry does: an ISO 3166-1 alpha-2 code in upper case,
// "" when it is not known (pkg/ipcountry, cached).
type Locator interface {
	Country(ctx context.Context, ip string) string
}

// Limits of the home side.
const (
	// stateCacheBytes and stateCacheTTL bound the cache of the files'
	// states upload.getFile decides by: a client asks for a file a
	// megabyte at a time, and MySQL is not asked every time.
	stateCacheBytes = 64 << 20
	stateCacheTTL   = time.Minute
	// healthCacheTTL: the health of a delivery is read again after it.
	healthCacheTTL = 2 * time.Second
	// reuploadsPerMinute is how many upload.reuploadCdnFile a user may call
	// a minute; then FLOOD_WAIT_X.
	reuploadsPerMinute = 30
)

// Home is the side of the CDN the methods of the clients use: the decision
// of upload.getFile, upload.getCdnFileHashes, upload.reuploadCdnFile and
// help.getCdnConfig (ADR 0071 §2, §9). It is safe for concurrent use.
type Home struct {
	store  Store
	set    *Settings
	sizer  Sizer
	caps   Caps
	router *Router
	now    func() time.Time

	// countries tells the country of a client, by which the routes pick
	// a delivery; nil when no record of the routes names countries.
	countries Locator

	files *stateCache

	reuploads *userLimiter
	// wake tells the preparer a file was queued; nil: it finds it by itself.
	wake func()
	// observer learns of the redirects; nil: none is told.
	observer RedirectObserver
}

// NewHome returns the Home of set over store. set is a parsed Config: its
// route names enabled deliveries. caps tells which clients take a redirect,
// countries the country of a client for the records of the routes that
// name countries; it may be nil only when none does.
func NewHome(store Store, set *Settings, sizer Sizer, caps Caps, countries Locator) (*Home, error) {
	router, err := NewRouter(set, NewStoreGate(store))
	if err != nil {
		return nil, err
	}
	if router.ByCountry() && countries == nil {
		return nil, errors.New("cdn: CDN_ROUTES names countries, and there is no source of the country of a client")
	}
	if !router.ByCountry() {
		countries = nil
	}
	return &Home{
		store:     store,
		set:       set,
		sizer:     sizer,
		caps:      caps,
		countries: countries,
		router:    router,
		now:       time.Now,
		files:     newStateCache(stateCacheBytes, stateCacheTTL),
		reuploads: newUserLimiter(reuploadsPerMinute, time.Minute),
	}, nil
}

// Observe makes o learn of every redirect from now on. It is called once,
// before the Home serves.
func (h *Home) Observe(o RedirectObserver) { h.observer = o }

// fileState is what upload.getFile decides a file by.
type fileState struct {
	file *File
	// ready are the targets the file is prepared for (cdn_replicas ready,
	// the key and the hashes stored).
	ready []int32
}

// Redirect decides a request of upload.getFile that Wants a redirect, after
// the checks of the request, for the client of the permanent auth key
// authKeyID at the address clientAddr (MD.ClientAddr): upload.fileCdnRedirect
// with a PLH1 file_token (token.go) when the client sent pluma_cdn, the
// file is prepared and a delivery of the record of the client's country
// takes it; nil when the file is served from home. A file that may go
// through the CDN and is not prepared is queued. The result is for the
// metrics and the tests, empty for a location that never goes through the
// CDN. The redirect is an optimization: a failure here is logged and the
// file served from home.
//
// Only a client that sent pluma_cdn gets a redirect (task 107, "Признак
// клиента"): cdn_supported alone is sent by every Android and tdesktop
// build (FileLoadOperation.java:2469, download_manager_mtproto.cpp:581),
// and an unpatched Android waits forever for a DC it does not know
// (jni/tgnet/ConnectionsManager.cpp:2585-2590).
func (h *Home) Redirect(ctx context.Context, authKeyID int64, clientAddr string, loc *mtproto.InputFileLocation, offset int64) (*mtproto.Upload_File, string) {
	key, ok := Key(loc)
	if !ok || !h.set.AllowsKind(key.Kind) {
		return nil, ""
	}
	res, dc, result, err := h.redirect(ctx, authKeyID, clientAddr, key, loc, offset)
	if err != nil {
		logx.WithContext(ctx).Errorf("cdn: upload.getFile served from home: %v", err)
	}
	metricRedirects.Inc(strconv.Itoa(int(dc)), result)
	return res, result
}

func (h *Home) redirect(ctx context.Context, authKeyID int64, clientAddr string, key FileKey, loc *mtproto.InputFileLocation, offset int64) (*mtproto.Upload_File, int32, string, error) {
	// Until the country is known, the metrics name the preferred delivery
	// of the record "*": it is looked up only for a client that may be
	// redirected.
	dc := h.router.First().ID
	if authKeyID == 0 {
		return nil, dc, ResultNoCaps, nil
	}
	v, err := h.caps.PlumaCdn(ctx, authKeyID)
	if err != nil {
		return nil, dc, ResultError, fmt.Errorf("pluma_cdn of the key: %w", err)
	}
	if v < 1 {
		return nil, dc, ResultNoCaps, nil
	}
	st, err := h.state(ctx, key, loc)
	if err != nil {
		return nil, dc, ResultError, err
	}
	if !st.file.Eligible {
		return nil, dc, ResultSmall, nil
	}
	country := ""
	if h.countries != nil {
		// An unknown country (no GeoIP answer, a failed lookup) is routed by
		// the record "*".
		country = h.countries.Country(ctx, clientAddr)
		dc = h.router.FirstOf(country).ID
	}
	now := h.now()
	d, result, err := h.router.Pick(ctx, authKeyID, country, now)
	if d == nil {
		return nil, dc, result, err
	}
	if !slices.Contains(st.ready, d.Target()) {
		return nil, d.ID, ResultNotReady, nil
	}
	hashes, err := Answer(st.file.Hashes, st.file.Size, offset)
	if err != nil {
		return nil, d.ID, ResultError, err
	}
	url, exp, err := d.Signer.SignURL(ObjectPath(st.file.Secrets.Token), now)
	if err != nil {
		return nil, d.ID, ResultError, err
	}
	token, err := EncodeRedirectToken(st.file.Secrets.Token, exp, url)
	if err != nil {
		return nil, d.ID, ResultError, err
	}
	h.router.Redirected(authKeyID, key, d.ID, now)
	if h.observer != nil {
		h.observer.Redirected(authKeyID, st.file, d.ID, now)
	}
	return mtproto.MakeTLUploadFileCdnRedirect(&mtproto.Upload_File{
		DcId:          d.ID,
		FileToken:     token,
		EncryptionKey: st.file.Secrets.Key,
		EncryptionIv:  st.file.Secrets.IV,
		FileHashes:    hashes,
	}).To_Upload_File(), d.ID, ResultRedirect, nil
}

// ServedHome takes a request of upload.getFile that does not Want a
// redirect, from the client of the permanent auth key authKeyID: a patched
// client asks without cdn_supported for a file it was redirected to when
// the delivery failed it (task 107, client contract p. 7-8), and its key
// then avoids that delivery (Router.FellBack). It reports whether the
// request was such a fallback.
func (h *Home) ServedHome(authKeyID int64, loc *mtproto.InputFileLocation) bool {
	key, ok := Key(loc)
	if !ok || authKeyID == 0 {
		return false
	}
	dc, ok := h.router.FellBack(authKeyID, key, h.now())
	if ok {
		metricFallbacks.Inc(strconv.Itoa(int(dc)))
	}
	return ok
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
		for _, target := range h.router.Targets() {
			r, err := h.store.Replica(ctx, f.RowId, target)
			if err != nil {
				return nil, err
			}
			if r != nil && r.State == StateReady && f.HasKey() && f.Hashes != nil {
				st.ready = append(st.ready, target)
				continue
			}
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

// ReportDevHTTP sets pluma_cdn_dev_http of each delivery of set, 1 for an
// http:// BaseURL, which only CDN_DEV_ALLOW_HTTP=1 of the e2e stand allows
// (an alert fires on it in production), and warns of each such delivery at
// start.
func ReportDevHTTP(set *Settings) {
	for _, d := range set.Deliveries {
		v := 0.0
		if d.InsecureHTTP {
			v = 1
			logx.Errorf("cdn: delivery %d (%s) serves over http:// (CDN_DEV_ALLOW_HTTP=1): only the e2e stand may run so, and released clients refuse it", d.ID, d.Name)
		}
		metricDevHTTP.Set(v, strconv.Itoa(int(d.ID)))
	}
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
