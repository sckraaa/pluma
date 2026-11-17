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

package svc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"math/rand"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/files/internal/config"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/webfile"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// noiseRenderer draws maps of random pixels, which PNG cannot compress, so
// that a map spans many parts; it records the requests and can fail.
type noiseRenderer struct {
	mu       sync.Mutex
	requests []renderRequest
	fail     error
}

type renderRequest struct {
	style string
	p     staticmap.Params
}

func (r *noiseRenderer) Render(_ context.Context, style string, p staticmap.Params) ([]byte, error) {
	r.mu.Lock()
	r.requests = append(r.requests, renderRequest{style, p})
	fail := r.fail
	r.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	img := image.NewNRGBA(image.Rect(0, 0, p.PixelWidth(), p.PixelHeight()))
	rand.New(rand.NewSource(int64(len(img.Pix)))).Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (r *noiseRenderer) seen() []renderRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]renderRequest(nil), r.requests...)
}

func (r *noiseRenderer) failWith(err error) {
	r.mu.Lock()
	r.fail = err
	r.mu.Unlock()
}

func testStore(t *testing.T) kv.Store {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the
	// failing connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	return kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

// testWebFiles is the Server of upload.getWebFile with the map source over
// r, as NewServiceContext registers it, and no cache of its own: the map
// source needs none.
func testWebFiles(t *testing.T, r staticmap.Renderer) *webfile.Server {
	t.Helper()
	maps, err := staticmap.NewService(staticmap.Config{URL: "http://maps:8080"}, testStore(t), r)
	if err != nil {
		t.Fatal(err)
	}
	s := webfile.NewServer(nil)
	s.Register(mtproto.Predicate_inputWebFileGeoPointLocation, NewGeoSource(maps))
	return s
}

func geoPointLocation(lat, long float64, w, h, zoom, scale int32) *mtproto.InputWebFileLocation {
	return mtproto.MakeTLInputWebFileGeoPointLocation(&mtproto.InputWebFileLocation{
		GeoPoint: mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: long}).To_InputGeoPoint(),
		W:        w, H: h, Zoom: zoom, Scale: scale,
	}).To_InputWebFileLocation()
}

// fetch loads a whole web file by parts of limit bytes, as Android does
// with 32 KiB (FileLoadOperation.java:1965), and checks that every part
// tells the same size and type, which tdesktop requires
// (file_download_mtproto.cpp:170-181).
func fetch(t *testing.T, s *webfile.Server, r webfile.Request, loc *mtproto.InputWebFileLocation, limit int32) ([]byte, *mtproto.Upload_WebFile) {
	t.Helper()
	ctx := context.Background()
	var (
		whole []byte
		first *mtproto.Upload_WebFile
	)
	for offset := int32(0); ; offset += limit {
		f, err := s.Get(ctx, r, loc, offset, limit)
		if err != nil {
			t.Fatalf("part at %d: %v", offset, err)
		}
		if first == nil {
			first = f
		} else if f.GetSize2() != first.GetSize2() || f.GetMimeType() != first.GetMimeType() || f.GetMtime() != first.GetMtime() {
			t.Fatalf("part at %d: size %d, type %q, mtime %d; the first part told %d, %q, %d",
				offset, f.GetSize2(), f.GetMimeType(), f.GetMtime(), first.GetSize2(), first.GetMimeType(), first.GetMtime())
		}
		whole = append(whole, f.GetBytes()...)
		if int32(len(f.GetBytes())) < limit {
			break
		}
	}
	return whole, first
}

// A map is served by parts cut from one image: they join into a PNG of
// w·scale × h·scale pixels, every part tells the same size, image/png and
// storage.filePng, and the whole map is rendered once for all its parts.
func TestGeoSourceParts(t *testing.T) {
	r := &noiseRenderer{}
	s := testWebFiles(t, r)
	req := webfile.Request{UserId: 7, LangCode: "ru"}
	// A bubble of Web K at devicePixelRatio 2 (components/wrappers/geo.ts:63-84).
	loc := geoPointLocation(55.755814, 37.617635, 277, 195, 16, 2)

	small, f := fetch(t, s, req, loc, 32<<10)
	if int(f.GetSize2()) != len(small) || len(small) <= 2*(32<<10) {
		t.Fatalf("size %d, %d bytes joined; want a map of several parts", f.GetSize2(), len(small))
	}
	if f.GetMimeType() != "image/png" || f.GetFileType().GetPredicateName() != mtproto.Predicate_storage_filePng {
		t.Errorf("type %q, %s", f.GetMimeType(), f.GetFileType().GetPredicateName())
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(small))
	if err != nil {
		t.Fatalf("the joined parts are no PNG: %v", err)
	}
	if cfg.Width != 554 || cfg.Height != 390 {
		t.Errorf("a map of %d×%d, want 554×390", cfg.Width, cfg.Height)
	}

	large, _ := fetch(t, s, req, loc, 128<<10)
	if !bytes.Equal(small, large) {
		t.Error("parts of 32 KiB and of 128 KiB join into different files")
	}
	if n := len(r.seen()); n != 1 {
		t.Errorf("%d renders for one map, want 1", n)
	}

	past, err := s.Get(context.Background(), req, loc, int32(len(small)), 32<<10)
	if err != nil || len(past.GetBytes()) != 0 || int(past.GetSize2()) != len(small) {
		t.Errorf("a part past the end: %d bytes, size %d, %v; want none, size %d", len(past.GetBytes()), past.GetSize2(), err, len(small))
	}
}

// The labels follow the language of the session (ADR 0063, p. 5), and the
// request reaches the maps service brought into range: the zoom as the
// client sent it and the scale tdesktop asks for kept (data/data_location.cpp:96).
func TestGeoSourceStyleAndParams(t *testing.T) {
	r := &noiseRenderer{}
	s := testWebFiles(t, r)
	for _, tc := range []struct {
		lang string
		lat  float64
	}{{"ru", 48.8566}, {"en", 48.8566}, {"be", 48.8567}, {"ru-RU", 48.8567}} {
		loc := geoPointLocation(tc.lat, 2.3522, 100, 100, 16, 4)
		if _, err := s.Get(context.Background(), webfile.Request{UserId: 7, LangCode: tc.lang}, loc, 0, 32<<10); err != nil {
			t.Fatalf("%s: %v", tc.lang, err)
		}
	}
	// Belarusian gets the Russian labels, so the map of ru-RU at the same
	// point is the one already drawn for it.
	seen := r.seen()
	want := []renderRequest{
		{"ru", staticmap.Params{Lat: 48.8566, Long: 2.3522, W: 100, H: 100, Zoom: 16, Scale: 4}},
		{"en", staticmap.Params{Lat: 48.8566, Long: 2.3522, W: 100, H: 100, Zoom: 16, Scale: 4}},
		{"ru", staticmap.Params{Lat: 48.8567, Long: 2.3522, W: 100, H: 100, Zoom: 16, Scale: 4}},
	}
	if len(seen) != len(want) {
		t.Fatalf("renders %+v, want %+v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("render %d: %+v, want %+v", i, seen[i], want[i])
		}
	}
}

// The errors of a map: a point or size the maps service cannot draw is
// LOCATION_INVALID before any render; a failed render is
// INTERNAL_SERVER_ERROR and is not kept, so the next display draws it
// again; the range is checked as for every web file.
func TestGeoSourceErrors(t *testing.T) {
	r := &noiseRenderer{}
	s := testWebFiles(t, r)
	ctx := context.Background()
	req := webfile.Request{UserId: 7, LangCode: "en"}

	for name, loc := range map[string]*mtproto.InputWebFileLocation{
		"no width":      geoPointLocation(52.52, 13.4, 0, 100, 15, 2),
		"longitude 200": geoPointLocation(52.52, 200, 100, 100, 15, 2),
		"empty point": mtproto.MakeTLInputWebFileGeoPointLocation(&mtproto.InputWebFileLocation{
			GeoPoint: mtproto.MakeTLInputGeoPointEmpty(nil).To_InputGeoPoint(),
			W:        100, H: 100, Zoom: 15, Scale: 2,
		}).To_InputWebFileLocation(),
	} {
		if _, err := s.Get(ctx, req, loc, 0, 32<<10); !errors.Is(err, mtproto.ErrLocationInvalid) {
			t.Errorf("%s: %v, want LOCATION_INVALID", name, err)
		}
	}
	if n := len(r.seen()); n != 0 {
		t.Errorf("%d renders of invalid locations", n)
	}

	loc := geoPointLocation(52.52, 13.4, 200, 100, 15, 2)
	if _, err := s.Get(ctx, req, loc, -1, 32<<10); !errors.Is(err, mtproto.ErrOffsetInvalid) {
		t.Errorf("offset -1: %v", err)
	}
	if _, err := s.Get(ctx, req, loc, 0, 2<<20); !errors.Is(err, mtproto.ErrLimitInvalid) {
		t.Errorf("limit 2 MiB: %v", err)
	}

	r.failWith(errors.New("connection refused"))
	if _, err := s.Get(ctx, req, loc, 0, 32<<10); !errors.Is(err, mtproto.ErrInternalServerError) {
		t.Errorf("a failed render: %v, want INTERNAL_SERVER_ERROR", err)
	}
	r.failWith(nil)
	if _, err := s.Get(ctx, req, loc, 0, 32<<10); err != nil {
		t.Errorf("the render after a failure: %v", err)
	}
	if n := len(r.seen()); n != 2 {
		t.Errorf("%d renders, want 2: the failed one is not kept", n)
	}
}

// Maps are on only with a URL; on, they need the Redis of KV, and a wrong
// Maps block stops the start.
func TestNewMaps(t *testing.T) {
	if m, err := newMaps(config.Config{}); m != nil || err != nil {
		t.Errorf("no URL: %v, %v; want maps off", m, err)
	}
	if _, err := newMaps(config.Config{Maps: staticmap.Config{URL: "http://maps:8080"}}); err == nil {
		t.Error("maps without KV were accepted")
	}
	mr := miniredis.RunT(t)
	kvConf := kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}}
	if _, err := newMaps(config.Config{KV: kvConf, Maps: staticmap.Config{URL: "http://maps:8080/styles"}}); err == nil {
		t.Error("a URL with a path was accepted")
	}
	if m, err := newMaps(config.Config{KV: kvConf, Maps: staticmap.Config{URL: "http://maps:8080"}}); m == nil || err != nil {
		t.Errorf("maps on: %v, %v", m, err)
	}
}
