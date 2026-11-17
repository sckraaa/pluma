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

//go:build integration

package staticmap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// The cache and the limit script run on the production Redis here (the unit
// tests run them on miniredis), and the renderer on the real tileserver-gl.

var itRedis *testinfra.Redis

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itRedis, err = testinfra.StartRedis(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

// itUserSeq keeps the limit counters of the tests apart, also with -count=N.
var itUserSeq atomic.Int64

func itUser() int64 { return 1_000_000 + itUserSeq.Add(1) }

func itStore() kv.Store { return kv.NewStore(itRedis.KvConf()) }

func TestCacheOnRedis(t *testing.T) {
	ctx := context.Background()
	c := mapCache{kv: itStore(), ttl: 90 * time.Second}
	// Every byte value, after a PNG signature: the entry must be binary-safe.
	body := append([]byte{}, pngSignature...)
	for i := 0; i < 4096; i++ {
		body = append(body, byte(i))
	}
	key := fmt.Sprintf("it-%d", time.Now().UnixNano())
	in := &Blob{Bytes: body, Mtime: -2}
	if err := c.put(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	out, ok, err := c.get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("get = %v, %v", ok, err)
	}
	if !bytes.Equal(out.Bytes, body) || out.Mtime != in.Mtime {
		t.Fatal("the entry came back changed")
	}
	ttl, err := redis.MustNewRedis(itRedis.RedisConf()).TtlCtx(ctx, cacheKeyPrefix+key)
	if err != nil {
		t.Fatal(err)
	}
	if ttl < 85 || ttl > 90 {
		t.Fatalf("TTL = %d, want 90", ttl)
	}
	if _, ok, err = c.get(ctx, key+"-missing"); ok || err != nil {
		t.Fatalf("get of a missing key = %v, %v", ok, err)
	}
}

func TestLimitOnRedisUnderRace(t *testing.T) {
	ctx := context.Background()
	l := limiter{kv: itStore(), limit: DefaultPerUserPerMinute}
	user := itUser()
	now := time.Now()
	var (
		wg      sync.WaitGroup
		passed  atomic.Int32
		refused atomic.Int32
		failed  atomic.Int32
	)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			over, wait, err := l.hit(ctx, user, now)
			switch {
			case err != nil:
				failed.Add(1)
			case over:
				if wait < 1 || wait > 60 {
					t.Errorf("wait = %d", wait)
				}
				refused.Add(1)
			default:
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != DefaultPerUserPerMinute || refused.Load() != 100-DefaultPerUserPerMinute || failed.Load() != 0 {
		t.Fatalf("passed %d, refused %d, failed %d", passed.Load(), refused.Load(), failed.Load())
	}
	key := fmt.Sprintf("%s%d:%d", limitKeyPrefix, user, now.Unix()/60)
	ttl, err := redis.MustNewRedis(itRedis.RedisConf()).TtlCtx(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 || ttl > 120 {
		t.Fatalf("counter TTL = %d", ttl)
	}
}

// Two vertical lines 2·lineDelta degrees of longitude apart, around
// lineCenter, on a white background: the distance between them in the
// image checks the meaning of zoom and scale without map data.
const (
	lineCenter = 10.0
	lineDelta  = 0.004
)

func linesStyle() []byte {
	line := func(lon float64) map[string]any {
		return map[string]any{
			"type":       "Feature",
			"properties": map[string]any{},
			"geometry":   map[string]any{"type": "LineString", "coordinates": [][]float64{{lon, -80}, {lon, 80}}},
		}
	}
	style := map[string]any{
		"version": 8,
		"name":    "lines",
		"sources": map[string]any{
			"lines": map[string]any{
				"type": "geojson",
				"data": map[string]any{
					"type":     "FeatureCollection",
					"features": []any{line(lineCenter - lineDelta), line(lineCenter + lineDelta)},
				},
			},
		},
		"layers": []any{
			map[string]any{"id": "background", "type": "background", "paint": map[string]any{"background-color": "#ffffff"}},
			map[string]any{"id": "lines", "type": "line", "source": "lines", "paint": map[string]any{"line-color": "#000000", "line-width": 2}},
		},
	}
	b, _ := json.Marshal(style)
	return b
}

// tileserverConfig is the configuration of the maps service
// (teamgramd/deploy/maps/config.json) cut to what the test needs: the
// scale and size limits and one rendered style.
const tileserverConfig = `{
  "options": {
    "paths": {"root": "/data", "styles": ""},
    "maxScaleFactor": 4,
    "maxSize": 2048,
    "serveAllStyles": false,
    "serveAllFonts": false,
    "allowRemoteMarkerIcons": false
  },
  "styles": {
    "lines": {"style": "lines.json", "serve_rendered": true, "serve_data": false}
  }
}`

// lineCenters returns the centers of the dark runs of the middle row of a
// PNG, weighted by darkness.
func lineCenters(t *testing.T, b []byte) []float64 {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	y := (bounds.Min.Y + bounds.Max.Y) / 2
	var (
		centers      []float64
		sum, weights float64
	)
	for x := bounds.Min.X; x <= bounds.Max.X; x++ {
		dark := 0.0
		if x < bounds.Max.X {
			g := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			dark = float64(255-g.Y) / 255
		}
		if dark > 0.05 {
			sum += float64(x) * dark
			weights += dark
			continue
		}
		if weights > 0 {
			centers = append(centers, sum/weights)
			sum, weights = 0, 0
		}
	}
	return centers
}

func TestTileserverGLZoomAndScale(t *testing.T) {
	ctx := context.Background()
	ts, err := testinfra.StartTileserverGL(ctx, map[string][]byte{
		"config.json": []byte(tileserverConfig),
		"lines.json":  linesStyle(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ts.Terminate(context.Background()) })
	r := NewHTTPRenderer(ts.URL, time.Minute)

	for _, scale := range []int32{1, 2} {
		p := Params{Lat: 0, Long: lineCenter, W: 512, H: 256, Zoom: 15, Scale: scale}
		b, err := r.Render(ctx, "lines", p)
		if err != nil {
			t.Fatalf("scale %d: %v", scale, err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width != p.PixelWidth() || cfg.Height != p.PixelHeight() {
			t.Fatalf("scale %d: image %+v, %v", scale, cfg, err)
		}
		centers := lineCenters(t, b)
		if len(centers) != 2 {
			t.Fatalf("scale %d: %d lines found at %v, want 2", scale, len(centers), centers)
		}
		// The clients' zoom: a world of 256·2^zoom points (Web K
		// components/wrappers/geo.ts:170-172), times the scale.
		want := 2 * lineDelta * 256 * math.Exp2(float64(p.Zoom)) / 360 * float64(scale)
		got := centers[1] - centers[0]
		t.Logf("scale %d: lines %.2f px apart, expected %.2f", scale, got, want)
		if math.Abs(got-want) > 1 {
			t.Fatalf("scale %d: lines %.2f px apart, want %.2f ± 1", scale, got, want)
		}
		// Centered on the requested point.
		if mid := (centers[0] + centers[1]) / 2; math.Abs(mid-float64(p.PixelWidth())/2) > 1 {
			t.Fatalf("scale %d: lines centered at %.2f, want %d", scale, mid, p.PixelWidth()/2)
		}
	}

	// @4x is served with maxScaleFactor 4 (tdesktop at 300%), @5x is not.
	p4 := Params{Lat: 0, Long: lineCenter, W: 256, H: 128, Zoom: 16, Scale: 4}
	if _, err = r.Render(ctx, "lines", p4); err != nil {
		t.Fatalf("@4x: %v", err)
	}
	u5 := strings.Replace(r.URL("lines", p4), "@4x", "@5x", 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u5, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("@5x answered %d, want 404", resp.StatusCode)
	}

	// The whole path: Service on Redis and the real renderer; the credit is
	// drawn, the map cached.
	store := itStore()
	s, err := NewService(Config{URL: ts.URL, Styles: "lines"}, store, r)
	if err != nil {
		t.Fatal(err)
	}
	loc := geoLocation(0, lineCenter, 277, 195, 15, 2)
	b, err := s.Get(ctx, itUser(), "en", loc)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 554, 390) {
		t.Fatalf("bounds = %v", img.Bounds())
	}
	// The plate is inside the bottom-left corner; the background is white,
	// so the text is what changes there.
	dark := 0
	for y := 390 - 2*attributionInset - 20; y < 390-2*attributionInset; y++ {
		for x := 2 * attributionInset; x < 2*attributionInset+60; x++ {
			if g := color.GrayModel.Convert(img.At(x, y)).(color.Gray); g.Y < 128 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Fatal("no credit text in the bottom-left corner")
	}
	p, _ := Normalize(loc)
	if _, ok, err := s.cache.get(ctx, Key("lines", p)); err != nil || !ok {
		t.Fatalf("the map is not cached: %v, %v", ok, err)
	}
}
