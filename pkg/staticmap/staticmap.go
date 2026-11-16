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

// Package staticmap serves the map previews of geo points
// (upload.getWebFile with inputWebFileGeoPointLocation, ADR 0063): it brings
// the request into range, renders the map with the maps service at home
// (tileserver-gl), adds the OpenStreetMap credit and keeps the image in Redis
// so that every part of one file is cut from the same bytes.
package staticmap

import (
	"context"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
)

// Blob is a rendered map: a PNG and the time it was drawn. Callers share it
// and must not change Bytes.
type Blob struct {
	Bytes []byte
	Mtime int32
}

// MimeType is the type of every map: Android asks for image/png
// (messenger/WebFile.java:42).
const MimeType = "image/png"

// Service renders and caches maps. It is safe for concurrent use.
type Service struct {
	styles   []string
	timeout  time.Duration
	renderer Renderer
	cache    mapCache
	limiter  limiter
	slots    *semaphore.Weighted
	flight   singleflight.Group
	now      func() time.Time
}

// New returns the Service of c on store, rendering with the maps service of
// c.URL; nil (and no error) when maps are off.
func New(c Config, store kv.Store) (*Service, error) {
	if !c.Enabled() {
		return nil, nil
	}
	s, err := c.parse()
	if err != nil {
		return nil, err
	}
	return newService(s, store, NewHTTPRenderer(s.url, s.timeout)), nil
}

// NewService returns the Service of an enabled c that renders with r.
func NewService(c Config, store kv.Store, r Renderer) (*Service, error) {
	if !c.Enabled() {
		return nil, errors.New("staticmap: MAPS_URL is empty")
	}
	s, err := c.parse()
	if err != nil {
		return nil, err
	}
	return newService(s, store, r), nil
}

func newService(s settings, store kv.Store, r Renderer) *Service {
	return &Service{
		styles:   s.styles,
		timeout:  s.timeout,
		renderer: r,
		cache:    mapCache{kv: store, ttl: s.cacheTTL},
		limiter:  limiter{kv: store, limit: int64(s.perUserPerMinute)},
		slots:    semaphore.NewWeighted(int64(s.concurrency)),
		now:      time.Now,
	}
}

// Get returns the map of an inputWebFileGeoPointLocation for userId, whose
// session speaks langCode. See GetParams for the errors.
func (s *Service) Get(ctx context.Context, userId int64, langCode string, loc *mtproto.InputWebFileLocation) (*Blob, error) {
	p, err := Normalize(loc)
	if err != nil {
		return nil, err
	}
	return s.GetParams(ctx, userId, langCode, p)
}

// GetParams returns the map of normalized p (Normalize) for userId, whose
// session speaks langCode. The errors are RPC errors: FLOOD_WAIT_X past the
// per-user limit, INTERNAL_SERVER_ERROR when the map cannot be rendered, or
// the error of ctx when the caller stops waiting. A failed render is not
// cached: the client asks again on its next display.
func (s *Service) GetParams(ctx context.Context, userId int64, langCode string, p Params) (*Blob, error) {
	style := StyleFor(langCode, s.styles)
	key := Key(style, p)

	b, ok, err := s.cache.get(ctx, key)
	switch {
	case err != nil:
		metricCache.Inc(cacheError)
		logx.WithContext(ctx).Errorf("staticmap: cache lookup for user %d: %v", userId, err)
	case ok:
		metricCache.Inc(cacheHit)
		return b, nil
	default:
		metricCache.Inc(cacheMiss)
	}

	// Only renders count: a cached map costs nothing.
	over, wait, err := s.limiter.hit(ctx, userId, s.now())
	switch {
	case err != nil:
		// The renderer is still guarded by the render slots; a Redis failure
		// must not take the maps down with it.
		metricLimitErrors.Inc()
		logx.WithContext(ctx).Errorf("staticmap: limit of user %d: %v", userId, err)
	case over:
		metricFlood.Inc()
		logx.WithContext(ctx).Infof("staticmap: user %d is over %d maps a minute, wait %ds", userId, s.limiter.limit, wait)
		return nil, mtproto.NewErrFloodWaitX(wait)
	}

	// Equal requests share one render. It runs detached from the caller, so
	// that a client that gives up does not waste the render of the others,
	// and the result is cached for its next try.
	ch := s.flight.DoChan(key, func() (any, error) {
		return s.render(context.WithoutCancel(ctx), userId, key, style, p)
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(*Blob), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// render waits for a render slot, renders, checks and credits the map and
// caches it.
func (s *Service) render(ctx context.Context, userId int64, key, style string, p Params) (*Blob, error) {
	waitCtx, cancel := context.WithTimeout(ctx, s.timeout)
	err := s.slots.Acquire(waitCtx, 1)
	cancel()
	if err != nil {
		metricRender.Inc(renderTimeout)
		logx.WithContext(ctx).Errorf("staticmap: no render slot for user %d within %s", userId, s.timeout)
		return nil, mtproto.ErrInternalServerError
	}
	defer s.slots.Release(1)

	// Another bff, or a flight that ended while this one queued, may have
	// stored it meanwhile.
	if b, ok, err := s.cache.get(ctx, key); err == nil && ok {
		return b, nil
	}

	start := time.Now()
	raw, err := s.renderWithTimeout(ctx, style, p)
	metricRenderSeconds.ObserveFloat(time.Since(start).Seconds())
	if err != nil {
		return nil, s.renderFailed(ctx, userId, err)
	}
	img, err := decodeMap(raw, p)
	if err != nil {
		return nil, s.renderFailed(ctx, userId, err)
	}
	if needsAttribution(p) {
		if raw, err = attribute(img, p.Scale); err != nil {
			metricRender.Inc(renderError)
			logx.WithContext(ctx).Errorf("staticmap: credit for user %d: %v", userId, err)
			return nil, mtproto.ErrInternalServerError
		}
	}
	metricRender.Inc(renderOK)

	b := &Blob{Bytes: raw, Mtime: int32(s.now().Unix())}
	if err = s.cache.put(ctx, key, b); err != nil {
		// Served uncached: the parts of this file may then come from
		// different renders, which are byte-equal only if the renderer is
		// deterministic; nothing better can be done without Redis.
		metricCache.Inc(cacheError)
		logx.WithContext(ctx).Errorf("staticmap: cache store for user %d: %v", userId, err)
	}
	return b, nil
}

func (s *Service) renderWithTimeout(ctx context.Context, style string, p Params) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	raw, err := s.renderer.Render(ctx, style, p)
	if err != nil && !errors.Is(err, ErrRenderTimeout) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = errors.Join(ErrRenderTimeout, err)
	}
	return raw, err
}

// renderFailed counts and logs a failed render by its class only (the
// errors of this package never carry the coordinates) and returns the RPC
// error.
func (s *Service) renderFailed(ctx context.Context, userId int64, err error) error {
	result := renderError
	switch {
	case errors.Is(err, ErrRenderTimeout):
		result = renderTimeout
	case errors.Is(err, ErrBadImage):
		result = renderInvalid
	}
	metricRender.Inc(result)
	logx.WithContext(ctx).Errorf("staticmap: render for user %d: %v", userId, err)
	return mtproto.ErrInternalServerError
}
