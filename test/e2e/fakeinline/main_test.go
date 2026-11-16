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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startStub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newStub().handler())
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, u string) (int, []byte, http.Header) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b, resp.Header
}

func plan(t *testing.T, srv *httptest.Server, v url.Values) int {
	t.Helper()
	resp, err := http.Post(srv.URL+"/e2e/plan?"+v.Encode(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func requestsOf(t *testing.T, srv *httptest.Server, v url.Values) []request {
	t.Helper()
	code, b, _ := get(t, srv.URL+"/e2e/requests?"+v.Encode())
	if code != http.StatusOK {
		t.Fatalf("requests: status %d", code)
	}
	var out []request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func searchURL(srv *httptest.Server, q, pageno, format string) string {
	return srv.URL + "/search?" + url.Values{
		"q": {q}, "categories": {"images"}, "format": {format}, "pageno": {pageno}, "safesearch": {"1"}, "language": {"de"},
	}.Encode()
}

// What the search answers is SearXNG's JSON of the image category, the
// fields app/bff/bots/internal/inline.Searxng reads: results with img_src
// and thumbnail_src on the host asked, the same for the same page, and the
// list of engines that failed.
func TestSearch(t *testing.T) {
	srv := startStub(t)
	code, b, h := get(t, searchURL(srv, "cat 1", "1", "json"))
	if code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, %s", code, h.Get("Content-Type"))
	}
	var a struct {
		Results []struct {
			ImgSrc       string `json:"img_src"`
			ThumbnailSrc string `json:"thumbnail_src"`
		} `json:"results"`
		UnresponsiveEngines [][]string `json:"unresponsive_engines"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Results) != defaultResults || a.UnresponsiveEngines == nil || len(a.UnresponsiveEngines) != 0 {
		t.Fatalf("%d results, unresponsive %v", len(a.Results), a.UnresponsiveEngines)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	seen := map[string]bool{}
	for i, r := range a.Results {
		img, err := url.Parse(r.ImgSrc)
		if err != nil || img.Scheme != "http" || img.Host != host || seen[img.Path] {
			t.Fatalf("img_src %q", r.ImgSrc)
		}
		seen[img.Path] = true
		// The path the e2e tests compute (fakePicturePath).
		sum := sha256.Sum256([]byte("cat 1\x001\x00" + strconv.Itoa(i)))
		if img.Path != "/pic/"+hex.EncodeToString(sum[:8])+".jpg" {
			t.Errorf("picture %d at %s", i, img.Path)
		}
		thumb, err := url.Parse(r.ThumbnailSrc)
		if err != nil || thumb.Path != img.Path || thumb.Query().Get("w") == "" {
			t.Errorf("thumbnail_src %q", r.ThumbnailSrc)
		}
	}
	_, again, _ := get(t, searchURL(srv, "cat 1", "1", "json"))
	_, next, _ := get(t, searchURL(srv, "cat 1", "2", "json"))
	if !bytes.Equal(b, again) || bytes.Equal(b, next) {
		t.Error("a page is not the same when asked again, or the next page is the same")
	}

	if code, _, _ := get(t, searchURL(srv, "cat 1", "1", "html")); code != http.StatusForbidden {
		t.Errorf("the html format: status %d, want 403", code)
	}
	reqs := requestsOf(t, srv, url.Values{"q": {"cat 1"}})
	if len(reqs) != 4 || reqs[0].Kind != "search" || reqs[0].PageNo != "1" || reqs[0].Language != "de" || reqs[0].SafeSearch != "1" ||
		reqs[0].Categories != "images" || reqs[0].Format != "json" || reqs[3].Format != "html" {
		t.Errorf("requests %+v", reqs)
	}
}

// A plan holds for its text or path only, for count requests, in order.
func TestPlan(t *testing.T) {
	srv := startStub(t)
	if code := plan(t, srv, url.Values{"q": {"down"}, "status": {"503"}, "count": {"1"}}); code != http.StatusNoContent {
		t.Fatalf("plan: status %d", code)
	}
	if code := plan(t, srv, url.Values{"q": {"down"}, "status": {"200"}, "count": {"1"}, "results": {"0"}, "unresponsive": {"1"}}); code != http.StatusNoContent {
		t.Fatalf("plan: status %d", code)
	}
	if code, _, _ := get(t, searchURL(srv, "other", "1", "json")); code != http.StatusOK {
		t.Errorf("another text: status %d", code)
	}
	if code, _, _ := get(t, searchURL(srv, "down", "1", "json")); code != http.StatusServiceUnavailable {
		t.Errorf("planned 503: status %d", code)
	}
	code, b, _ := get(t, searchURL(srv, "down", "1", "json"))
	if code != http.StatusOK || !strings.Contains(string(b), `"results":[]`) || !strings.Contains(string(b), `"unresponsive_engines":[["fake","timeout"]]`) {
		t.Errorf("planned no engine: status %d, %s", code, b)
	}
	if code, _, _ := get(t, searchURL(srv, "down", "1", "json")); code != http.StatusOK {
		t.Errorf("after the plan: status %d", code)
	}

	if code := plan(t, srv, url.Values{"path": {"/pic/a.jpg"}, "status": {"404"}, "count": {"1"}, "delay_ms": {"200"}}); code != http.StatusNoContent {
		t.Fatalf("plan: status %d", code)
	}
	start := time.Now()
	if code, _, _ := get(t, srv.URL+"/pic/a.jpg?w=10&h=10"); code != http.StatusNotFound || time.Since(start) < 200*time.Millisecond {
		t.Errorf("planned 404 after 200 ms: status %d after %v", code, time.Since(start))
	}
	if reqs := requestsOf(t, srv, url.Values{"path": {"/pic/a.jpg"}}); len(reqs) != 1 || reqs[0].Status != http.StatusNotFound || reqs[0].Kind != "file" {
		t.Errorf("requests of the path %+v", reqs)
	}
	if n := len(requestsOf(t, srv, url.Values{})); n != 5 {
		t.Errorf("%d requests in all, want 5", n)
	}

	for _, bad := range []url.Values{
		{"status": {"503"}, "count": {"1"}},
		{"q": {"x"}, "path": {"/y"}, "status": {"503"}, "count": {"1"}},
		{"q": {"x"}, "status": {"99"}, "count": {"1"}},
		{"q": {"x"}, "status": {"503"}, "count": {"0"}},
		{"q": {"x"}, "status": {"503"}, "count": {"1"}, "delay_ms": {"600000"}},
		{"q": {"x"}, "status": {"200"}, "count": {"1"}, "results": {"201"}},
	} {
		if code := plan(t, srv, bad); code != http.StatusBadRequest {
			t.Errorf("plan %v: status %d", bad, code)
		}
	}
}

// A picture is a JPEG of the size asked, the same bytes for the same name
// and size, another for another name.
func TestPicture(t *testing.T) {
	srv := startStub(t)
	code, b, h := get(t, srv.URL+"/pic/x.jpg?w=1000&h=1000")
	if code != http.StatusOK || h.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d, %s", code, h.Get("Content-Type"))
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != 1000 || cfg.Height != 1000 {
		t.Fatalf("%dx%d, %v", cfg.Width, cfg.Height, err)
	}
	if len(b) < 3*128<<10 {
		t.Errorf("a picture of 1000x1000 has %d bytes, fewer than three parts of 128 KB", len(b))
	}
	_, again, _ := get(t, srv.URL+"/pic/x.jpg?w=1000&h=1000")
	_, other, _ := get(t, srv.URL+"/pic/y.jpg?w=1000&h=1000")
	if !bytes.Equal(b, again) || bytes.Equal(b, other) {
		t.Error("the same picture differs, or another one is the same")
	}
	_, small, _ := get(t, srv.URL+"/pic/x.jpg")
	if cfg, err = jpeg.DecodeConfig(bytes.NewReader(small)); err != nil || cfg.Width != defaultW || cfg.Height != defaultH {
		t.Errorf("the default size: %dx%d, %v", cfg.Width, cfg.Height, err)
	}
	for _, q := range []string{"w=0", "h=2049", "w=a"} {
		if code, _, _ := get(t, srv.URL+"/pic/x.jpg?"+q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, code)
		}
	}
}
