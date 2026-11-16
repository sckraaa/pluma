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
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// encodeSolid encodes a w×h PNG of one color.
func encodeSolid(w, h int, c color.Color) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// solidPNG is a w×h PNG of one color.
func solidPNG(t testing.TB, w, h int, c color.Color) []byte {
	t.Helper()
	b, err := encodeSolid(w, h, c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var moscow = Params{Lat: 55.755814, Long: 37.617635, W: 277, H: 195, Zoom: 15, Scale: 2}

func TestHTTPRendererURL(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(t, 554, 390, color.White))
	}))
	defer srv.Close()

	r := NewHTTPRenderer(srv.URL+"/", time.Second)
	b, err := r.Render(context.Background(), "ru", moscow)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty image")
	}
	// Longitude first, the zoom as the client sent it, @2x for scale 2.
	if p := gotPath.Load(); p != "/styles/ru/static/37.617635,55.755814,15/277x195@2x.png" {
		t.Fatalf("path = %v", p)
	}

	one := Params{Lat: -33.856784, Long: -0.5, W: 100, H: 120, Zoom: 13, Scale: 1}
	if got := r.URL("en", one); got != srv.URL+"/styles/en/static/-0.500000,-33.856784,13/100x120.png" {
		t.Fatalf("URL with scale 1 = %s", got)
	}
}

func TestHTTPRendererFailures(t *testing.T) {
	good := solidPNG(t, 554, 390, color.White)
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://tile.openstreetmap.org/x.png", http.StatusFound)
		}, ErrRenderFailed},
		{"400", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Invalid center", http.StatusBadRequest)
		}, ErrRenderFailed},
		{"503", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}, ErrRenderFailed},
		{"not png type", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(good)
		}, ErrBadImage},
		{"not png body", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>not a map</html>"))
		}, ErrBadImage},
		{"png of another size", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(solidPNG(t, 277, 195, color.White))
		}, ErrBadImage},
		{"too large", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(good)
			_, _ = w.Write(make([]byte, maxAnswer))
		}, ErrBadImage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			_, err := NewHTTPRenderer(srv.URL, time.Second).Render(context.Background(), "ru", moscow)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "37.6") || strings.Contains(err.Error(), "55.7") {
				t.Fatalf("error carries the coordinates: %v", err)
			}
			if strings.Contains(err.Error(), "554") || strings.Contains(err.Error(), "277") {
				t.Fatalf("error carries the size of the map: %v", err)
			}
		})
	}
}

func TestHTTPRendererTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	start := time.Now()
	_, err := NewHTTPRenderer(srv.URL, 100*time.Millisecond).Render(context.Background(), "ru", moscow)
	if !errors.Is(err, ErrRenderTimeout) {
		t.Fatalf("error = %v, want a timeout", err)
	}
	if strings.Contains(err.Error(), "37.6") {
		t.Fatalf("error carries the coordinates: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %s", d)
	}
}

func TestHTTPRendererDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	_, err := NewHTTPRenderer(addr, time.Second).Render(context.Background(), "ru", moscow)
	if !errors.Is(err, ErrRenderFailed) {
		t.Fatalf("error = %v, want ErrRenderFailed", err)
	}
	if strings.Contains(err.Error(), "37.6") {
		t.Fatalf("error carries the coordinates: %v", err)
	}
}

func TestHTTPRendererIgnoresProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(t, 554, 390, color.White))
	}))
	defer srv.Close()
	// httptest listens on 127.0.0.1, which ProxyFromEnvironment would skip
	// anyway; a renderer must not consult the environment at all.
	if NewHTTPRenderer(srv.URL, time.Second).client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("the renderer's transport uses a proxy")
	}
	if _, err := NewHTTPRenderer(srv.URL, time.Second).Render(context.Background(), "ru", moscow); err != nil {
		t.Fatal(err)
	}
}
