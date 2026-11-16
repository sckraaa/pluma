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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/staticmap"
)

// fakemaps must answer the requests of bff's own renderer: a stand whose
// fake drifts from staticmap.HTTPRenderer would test nothing.

func startStub(t *testing.T) (*httptest.Server, *staticmap.HTTPRenderer) {
	t.Helper()
	srv := httptest.NewServer(newStub("ru,en").handler())
	t.Cleanup(srv.Close)
	return srv, staticmap.NewHTTPRenderer(srv.URL, 2*time.Second)
}

func requestsOf(t *testing.T, srv *httptest.Server, query string) []request {
	t.Helper()
	resp, err := http.Get(srv.URL + "/e2e/requests" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []request
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func planPoint(t *testing.T, srv *httptest.Server, p staticmap.Params, status, delayMs, count int) {
	t.Helper()
	resp, err := http.Post(fmt.Sprintf("%s/e2e/plan?lon=%.6f&lat=%.6f&status=%d&delay_ms=%d&count=%d",
		srv.URL, p.Long, p.Lat, status, delayMs, count), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("plan: status %d", resp.StatusCode)
	}
}

func firstPixelSeq(t *testing.T, b []byte) int {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	c := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA)
	return int(c.R)<<16 | int(c.G)<<8 | int(c.B)
}

func TestRendersWhatStaticmapAsks(t *testing.T) {
	srv, r := startStub(t)
	ctx := context.Background()
	// The Red Square at Android's zoom and a bubble of 277×195 at 2x.
	p := staticmap.Params{Lat: 55.755814, Long: 37.617635, W: 277, H: 195, Zoom: 15, Scale: 2}
	b, err := r.Render(ctx, "ru", p)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(b) <= 128<<10 {
		t.Errorf("a map of %d bytes fits one part of 128 KB", len(b))
	}
	if seq := firstPixelSeq(t, b); seq != 1 {
		t.Errorf("first pixel says request %d, want 1", seq)
	}
	one := staticmap.Params{Lat: -33.5, Long: -70.25, W: 100, H: 100, Zoom: 13, Scale: 1}
	b, err = r.Render(ctx, "en", one)
	if err != nil {
		t.Fatalf("render at 1x: %v", err)
	}
	if seq := firstPixelSeq(t, b); seq != 2 {
		t.Errorf("first pixel says request %d, want 2", seq)
	}

	got := requestsOf(t, srv, "?lon=37.617635&lat=55.755814")
	want := []request{{Seq: 1, Style: "ru", Lon: 37.617635, Lat: 55.755814, Zoom: 15, W: 277, H: 195, Scale: 2, Status: 200}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("requests of the point: %+v, want %+v", got, want)
	}
	if all := requestsOf(t, srv, ""); len(all) != 2 || all[1].Scale != 1 || all[1].Style != "en" {
		t.Errorf("all requests: %+v", all)
	}
}

func TestRefusesAsTileserverGL(t *testing.T) {
	srv, r := startStub(t)
	ctx := context.Background()
	p := staticmap.Params{Lat: 1, Long: 2, W: 256, H: 128, Zoom: 16, Scale: 4}
	if _, err := r.Render(ctx, "de", p); !errors.Is(err, staticmap.ErrRenderFailed) || !strings.Contains(err.Error(), "404") {
		t.Errorf("a style not served: %v", err)
	}
	if _, err := r.Render(ctx, "ru", p); err != nil {
		t.Errorf("@4x: %v", err)
	}
	for path, status := range map[string]int{
		"/styles/ru/static/2.000000,1.000000,16/256x128@5x.png":   http.StatusNotFound,
		"/styles/ru/static/2.000000,1.000000,16/1024x1024@4x.png": http.StatusBadRequest,
		"/styles/ru/static/2.000000,1.000000/256x128.png":         http.StatusBadRequest,
		"/styles/ru/static/2.000000,1.000000,16/256x128.jpg":      http.StatusBadRequest,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != status {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, status)
		}
	}
}

func TestPlannedAnswers(t *testing.T) {
	srv, r := startStub(t)
	ctx := context.Background()
	p := staticmap.Params{Lat: 48.856613, Long: 2.352222, W: 200, H: 150, Zoom: 15, Scale: 1}
	planPoint(t, srv, p, http.StatusServiceUnavailable, 0, 1)
	if _, err := r.Render(ctx, "en", p); !errors.Is(err, staticmap.ErrRenderFailed) || !strings.Contains(err.Error(), "503") {
		t.Errorf("planned 503: %v", err)
	}
	if _, err := r.Render(ctx, "en", p); err != nil {
		t.Errorf("after the plan: %v", err)
	}
	// Another point is not affected by a plan.
	other := p
	other.Long += 0.000001
	planPoint(t, srv, p, http.StatusOK, 3000, 1)
	if _, err := r.Render(ctx, "en", other); err != nil {
		t.Errorf("another point: %v", err)
	}
	if _, err := r.Render(ctx, "en", p); !errors.Is(err, staticmap.ErrRenderTimeout) {
		t.Errorf("planned delay past the timeout: %v", err)
	}
	got := requestsOf(t, srv, "?lon=2.352222&lat=48.856613")
	if len(got) != 3 || got[0].Status != 503 || got[1].Status != 200 || got[2].Status != 200 {
		t.Errorf("requests of the point: %+v", got)
	}
}
