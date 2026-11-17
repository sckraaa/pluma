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

package staticmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
)

// fakeRenderer draws solid maps and counts what it is asked.
type fakeRenderer struct {
	calls   atomic.Int32
	active  atomic.Int32
	peak    atomic.Int32
	release chan struct{} // if set, each render waits for a value
	// ignoreCtx makes a render wait for release past its deadline, holding
	// its slot.
	ignoreCtx bool
	fail      atomic.Pointer[error]
}

func (f *fakeRenderer) Render(ctx context.Context, style string, p Params) ([]byte, error) {
	f.calls.Add(1)
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		peak := f.peak.Load()
		if n <= peak || f.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	switch {
	case f.release != nil && f.ignoreCtx:
		<-f.release
	case f.release != nil:
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := f.fail.Load(); err != nil {
		return nil, *err
	}
	return encodeSolid(p.PixelWidth(), p.PixelHeight(), color.NRGBA{R: 0xe0, G: 0xe8, B: 0xd8, A: 0xff})
}

func (f *fakeRenderer) failWith(err error) { f.fail.Store(&err) }

func newTestKV(t *testing.T) (*miniredis.Miniredis, kv.Store) {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the
	// failing connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	return mr, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

// testClock is a fixed time 10 seconds into a minute.
var testClock = time.Unix(1_800_000_000-1_800_000_000%60+10, 0)

func testService(t *testing.T, c Config, r Renderer) (*Service, *miniredis.Miniredis) {
	t.Helper()
	mr, store := newTestKV(t)
	if c.URL == "" {
		c.URL = "http://maps:8080"
	}
	s, err := NewService(c, store, r)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return testClock }
	return s, mr
}

func rpcMessage(err error) string { return status.Convert(err).Message() }

func TestNewOff(t *testing.T) {
	s, err := New(Config{}, nil)
	if s != nil || err != nil {
		t.Fatalf("New of an empty config = %v, %v; want nil, nil", s, err)
	}
	if _, err = New(Config{URL: "http://maps/x"}, nil); err == nil {
		t.Fatal("New accepted a bad config")
	}
}

func TestServiceRendersAndCaches(t *testing.T) {
	r := &fakeRenderer{}
	s, mr := testService(t, Config{}, r)
	ctx := context.Background()
	loc := geoLocation(55.755814, 37.617635, 277, 195, 15, 2)

	b, err := s.Get(ctx, 1, "ru", loc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b.Bytes, pngSignature) || b.Mtime != int32(testClock.Unix()) {
		t.Fatalf("blob = %d bytes, mtime %d", len(b.Bytes), b.Mtime)
	}
	p, _ := Normalize(loc)
	if !mr.Exists(cacheKeyPrefix + Key("ru", p)) {
		t.Fatal("the map is not cached")
	}
	if ttl := mr.TTL(cacheKeyPrefix + Key("ru", p)); ttl != DefaultCacheTTL {
		t.Fatalf("cache TTL = %s", ttl)
	}

	// Another user, another part: the same bytes, no render, no count.
	again, err := s.Get(ctx, 2, "ru-RU", loc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.Bytes, b.Bytes) || again.Mtime != b.Mtime {
		t.Fatal("a cached map differs from the rendered one")
	}
	if n := r.calls.Load(); n != 1 {
		t.Fatalf("renders = %d, want 1", n)
	}
	if keys := mr.Keys(); len(keys) != 2 { // the map and user 1's counter
		t.Fatalf("keys = %v", keys)
	}

	// Another language is another style and another map.
	if _, err = s.Get(ctx, 2, "en", loc); err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 2 {
		t.Fatalf("renders = %d, want 2", n)
	}
}

func TestServiceRefusesBadLocation(t *testing.T) {
	r := &fakeRenderer{}
	s, _ := testService(t, Config{}, r)
	if _, err := s.Get(context.Background(), 1, "ru", geoLocation(0, 200, 100, 100, 15, 1)); !errors.Is(err, mtproto.ErrLocationInvalid) {
		t.Fatalf("error = %v", err)
	}
	if r.calls.Load() != 0 {
		t.Fatal("a bad location was rendered")
	}
}

func TestServiceCacheHitIsNotLimited(t *testing.T) {
	r := &fakeRenderer{}
	s, _ := testService(t, Config{PerUserPerMinute: "1"}, r)
	ctx := context.Background()
	loc := geoLocation(10, 10, 100, 100, 15, 1)
	if _, err := s.Get(ctx, 7, "en", loc); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Get(ctx, 7, "en", loc); err != nil {
			t.Fatalf("cached request %d: %v", i, err)
		}
	}
	_, err := s.Get(ctx, 7, "en", geoLocation(11, 11, 100, 100, 15, 1))
	if rpcMessage(err) != "FLOOD_WAIT_50" {
		t.Fatalf("second render error = %v, want FLOOD_WAIT_50", err)
	}
}

func TestServiceFloodWait(t *testing.T) {
	r := &fakeRenderer{}
	s, _ := testService(t, Config{}, r)
	ctx := context.Background()
	for i := 0; i < DefaultPerUserPerMinute; i++ {
		if _, err := s.Get(ctx, 3, "ru", geoLocation(float64(i)/100, 0, 100, 100, 15, 1)); err != nil {
			t.Fatalf("render %d: %v", i+1, err)
		}
	}
	_, err := s.Get(ctx, 3, "ru", geoLocation(50, 50, 100, 100, 15, 1))
	if rpcMessage(err) != "FLOOD_WAIT_50" {
		t.Fatalf("render 61 error = %v, want FLOOD_WAIT_50", err)
	}
	if n := r.calls.Load(); n != DefaultPerUserPerMinute {
		t.Fatalf("renders = %d", n)
	}
	// Other users are not affected; the next minute starts afresh.
	if _, err = s.Get(ctx, 4, "ru", geoLocation(50, 50, 100, 100, 15, 1)); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return testClock.Add(time.Minute) }
	if _, err = s.Get(ctx, 3, "ru", geoLocation(51, 51, 100, 100, 15, 1)); err != nil {
		t.Fatal(err)
	}
}

// downKV is a Redis that fails every command the service sends; calls
// counts them. It stands for a failing miniredis where requests fail at
// once: go-zero v1.10.3 formats the error window of an open breaker without
// its lock (core/breaker/breaker.go, errorWindow.String reads count before
// Lock; v1.11.0 takes the lock first), which the race detector reports.
type downKV struct {
	kv.Store
	calls atomic.Int64
}

var errRedisDown = errors.New("ERR down")

func (d *downKV) GetCtx(context.Context, string) (string, error) {
	d.calls.Add(1)
	return "", errRedisDown
}

func (d *downKV) SetexCtx(context.Context, string, string, int) error {
	d.calls.Add(1)
	return errRedisDown
}

func (d *downKV) EvalCtx(context.Context, string, string, ...any) (any, error) {
	d.calls.Add(1)
	return nil, errRedisDown
}

func TestServiceCollapsesEqualRequests(t *testing.T) {
	r := &fakeRenderer{release: make(chan struct{})}
	// Without Redis nothing is cached: only the shared render keeps the
	// ten requests to one.
	down := &downKV{}
	s, err := NewService(Config{URL: "http://maps:8080"}, down, r)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return testClock }
	loc := geoLocation(55.755814, 37.617635, 277, 195, 15, 2)

	const n = 10
	var (
		wg      sync.WaitGroup
		started sync.WaitGroup
		blobs   [n]*Blob
		errs    [n]error
	)
	started.Add(n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.Done()
			blobs[i], errs[i] = s.Get(context.Background(), int64(100+i), "ru", loc)
		}()
	}
	started.Wait()
	waitFor(t, func() bool { return r.calls.Load() == 1 })
	time.Sleep(200 * time.Millisecond) // let the others join the flight
	close(r.release)
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if !bytes.Equal(blobs[i].Bytes, blobs[0].Bytes) {
			t.Fatalf("request %d got other bytes", i)
		}
	}
	if c := r.calls.Load(); c != 1 {
		t.Fatalf("renders = %d, want 1", c)
	}
	// Every request looked up the cache and counted its render.
	if c := down.calls.Load(); c < 2*n {
		t.Fatalf("Redis commands = %d, want at least %d", c, 2*n)
	}
}

func TestServiceLimitsConcurrentRenders(t *testing.T) {
	r := &fakeRenderer{release: make(chan struct{})}
	s, _ := testService(t, Config{Concurrency: "4"}, r)
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Get(context.Background(), int64(i), "ru", geoLocation(float64(i), 0, 100, 100, 15, 1))
		}()
	}
	waitFor(t, func() bool { return r.active.Load() == 4 })
	time.Sleep(200 * time.Millisecond)
	if c := r.calls.Load(); c != 4 {
		t.Fatalf("renders started = %d, want 4 (the fifth must wait)", c)
	}
	r.release <- struct{}{}
	waitFor(t, func() bool { return r.calls.Load() == 5 })
	close(r.release)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if p := r.peak.Load(); p != 4 {
		t.Fatalf("peak concurrent renders = %d, want 4", p)
	}
}

func TestServiceSlotWaitTimesOut(t *testing.T) {
	r := &fakeRenderer{release: make(chan struct{}), ignoreCtx: true}
	s, _ := testService(t, Config{Concurrency: "1", Timeout: "1s"}, r)
	done := make(chan error, 1)
	go func() {
		_, err := s.Get(context.Background(), 1, "ru", geoLocation(1, 1, 100, 100, 15, 1))
		done <- err
	}()
	waitFor(t, func() bool { return r.active.Load() == 1 })
	_, err := s.Get(context.Background(), 2, "ru", geoLocation(2, 2, 100, 100, 15, 1))
	if !errors.Is(err, mtproto.ErrInternalServerError) {
		t.Fatalf("queued request error = %v, want INTERNAL_SERVER_ERROR", err)
	}
	close(r.release)
	if err = <-done; err != nil {
		t.Fatalf("first request: %v", err)
	}
	if c := r.calls.Load(); c != 1 {
		t.Fatalf("renders = %d, want 1", c)
	}
}

func TestServiceRedisDown(t *testing.T) {
	r := &fakeRenderer{}
	s, mr := testService(t, Config{}, r)
	mr.SetError("ERR down")
	loc := geoLocation(55.755814, 37.617635, 277, 195, 15, 2)
	b, err := s.Get(context.Background(), 1, "ru", loc)
	if err != nil {
		t.Fatalf("Get without Redis: %v", err)
	}
	if !bytes.HasPrefix(b.Bytes, pngSignature) {
		t.Fatal("not a PNG")
	}
	mr.SetError("")
	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("keys after a Redis failure = %v", keys)
	}
}

func TestServiceFailedRenderIsNotCached(t *testing.T) {
	r := &fakeRenderer{}
	s, mr := testService(t, Config{}, r)
	loc := geoLocation(55.755814, 37.617635, 277, 195, 15, 2)
	r.failWith(fmt.Errorf("%w: status 503", ErrRenderFailed))
	_, err := s.Get(context.Background(), 1, "ru", loc)
	if !errors.Is(err, mtproto.ErrInternalServerError) {
		t.Fatalf("error = %v, want INTERNAL_SERVER_ERROR", err)
	}
	p, _ := Normalize(loc)
	if mr.Exists(cacheKeyPrefix + Key("ru", p)) {
		t.Fatal("a failed render is cached")
	}
	r.fail.Store(nil)
	if _, err = s.Get(context.Background(), 1, "ru", loc); err != nil {
		t.Fatal(err)
	}
	if c := r.calls.Load(); c != 2 {
		t.Fatalf("renders = %d, want 2", c)
	}
}

func TestServiceRefusesBadImage(t *testing.T) {
	s, mr := testService(t, Config{}, badRenderer{})
	_, err := s.Get(context.Background(), 1, "ru", geoLocation(1, 1, 100, 100, 15, 1))
	if !errors.Is(err, mtproto.ErrInternalServerError) {
		t.Fatalf("error = %v", err)
	}
	for _, k := range mr.Keys() {
		if len(k) > len(cacheKeyPrefix) && k[:len(cacheKeyPrefix)] == cacheKeyPrefix {
			t.Fatalf("a bad image is cached under %s", k)
		}
	}
}

// badRenderer answers a PNG of half the size.
type badRenderer struct{}

func (badRenderer) Render(_ context.Context, _ string, p Params) ([]byte, error) {
	return encodeSolid(p.PixelWidth()/2, p.PixelHeight()/2, color.White)
}

func TestServiceCallerGivesUp(t *testing.T) {
	r := &fakeRenderer{release: make(chan struct{})}
	s, mr := testService(t, Config{}, r)
	loc := geoLocation(3, 3, 100, 100, 15, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.Get(ctx, 1, "ru", loc)
		done <- err
	}()
	waitFor(t, func() bool { return r.active.Load() == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	// The render goes on and is cached for the next try.
	close(r.release)
	p, _ := Normalize(loc)
	waitFor(t, func() bool { return mr.Exists(cacheKeyPrefix + Key("ru", p)) })
}

func TestCacheIgnoresDamagedEntry(t *testing.T) {
	mr, store := newTestKV(t)
	c := mapCache{kv: store, ttl: time.Hour}
	if err := mr.Set(cacheKeyPrefix+"k", "\x00\x00\x00\x01GIF89a.."); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.get(context.Background(), "k"); ok || err != nil {
		t.Fatalf("get of a damaged entry = %v, %v", ok, err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
