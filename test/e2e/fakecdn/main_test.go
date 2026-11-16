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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testAuth  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testKey   = "fakecdn-test-key"
	testPath  = "/o/00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	testSlice = 1000
	testSize  = 2500
)

// TestGcoreToken: the token is Gcore's, checked against the output of the
// openssl command of Gcore's Secure Token page
// (echo -n '<expires><path> <key>' | openssl md5 -binary | openssl base64 |
// tr +/ -_ | tr -d =), run on these values.
func TestGcoreToken(t *testing.T) {
	for _, tt := range []struct{ expires, path, key, want string }{
		{"2147483647", "/images/1.jpg", "secret_key", "vHEnxR8VaxpEDgyNzJuWwA"},
		{"1794870000", testPath, testKey, "6diGKTW9zd8OVP4AjXkqAQ"},
	} {
		if got := gcoreToken(tt.expires, tt.path, tt.key); got != tt.want {
			t.Errorf("gcoreToken(%s, %s) = %s, want %s", tt.expires, tt.path, got, tt.want)
		}
	}
}

// originRequest is what the origin stub saw.
type originRequest struct {
	rangeHdr, query, auth, acceptEncoding string
}

type stubOrigin struct {
	srv          *httptest.Server
	content      []byte
	cacheControl string

	mu       sync.Mutex
	requests []originRequest
	wantAuth string        // the X-Pluma-Origin-Auth it accepts
	status   int           // != 0: answer this instead
	delay    time.Duration // before answering
}

func newOrigin(t *testing.T) *stubOrigin {
	t.Helper()
	o := &stubOrigin{content: make([]byte, testSize), cacheControl: "public, max-age=2592000, immutable", wantAuth: testAuth}
	for i := range o.content {
		o.content[i] = byte(i*7 + i/256)
	}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		o.requests = append(o.requests, originRequest{r.Header.Get("Range"), r.URL.RawQuery, r.Header.Get(originAuthHeader), r.Header.Get("Accept-Encoding")})
		status, delay, wantAuth := o.status, o.delay, o.wantAuth
		o.mu.Unlock()
		time.Sleep(delay)
		if r.Header.Get(originAuthHeader) != wantAuth {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Cache-Control", o.cacheControl)
		http.ServeContent(w, r, "", time.Unix(1794800000, 0), bytes.NewReader(o.content))
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *stubOrigin) set(status int, delay time.Duration) {
	o.mu.Lock()
	o.status, o.delay = status, delay
	o.mu.Unlock()
}

func (o *stubOrigin) seen() []originRequest {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]originRequest(nil), o.requests...)
}

type stand struct {
	origin *stubOrigin
	c      *cdn
	cdn    *httptest.Server
	ctl    *httptest.Server

	mu  sync.Mutex
	now time.Time
}

func (s *stand) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

func (s *stand) advance(d time.Duration) {
	s.mu.Lock()
	s.now = s.now.Add(d)
	s.mu.Unlock()
}

func newStand(t *testing.T, mode string) *stand {
	t.Helper()
	o := newOrigin(t)
	c := newCDN(config{origin: o.srv.URL, originAuth: testAuth, mode: mode, tokenKey: testKey, sliceSize: testSlice, cacheBytes: 1 << 20})
	s := &stand{origin: o, c: c, now: time.Unix(1794860000, 0)}
	c.now = s.clock
	s.cdn = httptest.NewServer(c)
	s.ctl = httptest.NewServer(c.control())
	t.Cleanup(s.cdn.Close)
	t.Cleanup(s.ctl.Close)
	return s
}

// signed is a gcore-md5 URL of path valid for an hour.
func (s *stand) signed(path string) string {
	exp := strconv.FormatInt(s.clock().Unix()+3600, 10)
	return s.cdn.URL + path + "?md5=" + gcoreToken(exp, path, testKey) + "&expires=" + exp
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

func get(t *testing.T, url, rangeHdr string) answer {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{resp.StatusCode, resp.Header, body}
}

func (s *stand) ctlPost(t *testing.T, path string) {
	t.Helper()
	resp, err := http.Post(s.ctl.URL+path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST %s: %d", path, resp.StatusCode)
	}
}

func (s *stand) stats(t *testing.T) stats {
	t.Helper()
	resp, err := http.Get(s.ctl.URL + "/_ctl/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st stats
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestTokenChecked(t *testing.T) {
	s := newStand(t, modeGcoreMD5)
	past := strconv.FormatInt(s.clock().Unix()-1, 10)
	for name, url := range map[string]string{
		"no token":   s.cdn.URL + testPath,
		"wrong":      s.cdn.URL + testPath + "?md5=AAAAAAAAAAAAAAAAAAAAAA&expires=" + strconv.FormatInt(s.clock().Unix()+60, 10),
		"expired":    s.cdn.URL + testPath + "?md5=" + gcoreToken(past, testPath, testKey) + "&expires=" + past,
		"other path": strings.Replace(s.signed(testPath), "/o/00", "/o/01", 1),
	} {
		if a := get(t, url, "bytes=0-9"); a.status != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, a.status)
		}
	}
	if n := len(s.origin.seen()); n != 0 {
		t.Errorf("refused requests reached the origin %d times", n)
	}
	if a := get(t, s.signed(testPath), "bytes=0-9"); a.status != http.StatusPartialContent {
		t.Errorf("signed: %d, want 206", a.status)
	}
	s.ctlPost(t, "/_ctl/notoken?on=1")
	if a := get(t, s.cdn.URL+testPath, "bytes=0-9"); a.status != http.StatusPartialContent {
		t.Errorf("notoken: %d, want 206", a.status)
	}
}

func TestRangesFromSlices(t *testing.T) {
	s := newStand(t, modeGcoreMD5)
	url := s.signed(testPath)
	for _, tt := range []struct {
		rangeHdr     string
		start, end   int64
		status       int
		contentRange string
	}{
		{"bytes=0-9", 0, 9, 206, "bytes 0-9/2500"},
		{"bytes=990-2010", 990, 2010, 206, "bytes 990-2010/2500"}, // three slices
		{"bytes=2400-", 2400, 2499, 206, "bytes 2400-2499/2500"},
		{"bytes=2490-9999", 2490, 2499, 206, "bytes 2490-2499/2500"},
		{"bytes=-7", 2493, 2499, 206, "bytes 2493-2499/2500"},
		{"", 0, 2499, 200, ""},
		{"bytes=0-1,5-6", 0, 2499, 200, ""},
	} {
		a := get(t, url, tt.rangeHdr)
		if a.status != tt.status || a.header.Get("Content-Range") != tt.contentRange {
			t.Errorf("%q: %d %q, want %d %q", tt.rangeHdr, a.status, a.header.Get("Content-Range"), tt.status, tt.contentRange)
			continue
		}
		if !bytes.Equal(a.body, s.origin.content[tt.start:tt.end+1]) {
			t.Errorf("%q: wrong bytes", tt.rangeHdr)
		}
		if a.header.Get("Content-Length") != strconv.FormatInt(tt.end-tt.start+1, 10) || a.header.Get("ETag") != `"v1"` {
			t.Errorf("%q: headers %v", tt.rangeHdr, a.header)
		}
	}
	a := get(t, url, "bytes=2500-2600")
	if a.status != http.StatusRequestedRangeNotSatisfiable || a.header.Get("Content-Range") != "bytes */2500" {
		t.Errorf("past the end: %d %q", a.status, a.header.Get("Content-Range"))
	}

	// Each slice came from the origin once, with the origin's header, the
	// client's query and no Accept-Encoding.
	seen := s.origin.seen()
	if len(seen) != 3 {
		t.Fatalf("origin requests: %+v, want the three slices once", seen)
	}
	query := url[strings.Index(url, "?")+1:]
	for i, r := range seen {
		if r.auth != testAuth || r.query != query || r.acceptEncoding != "" {
			t.Errorf("origin request %d: %+v", i, r)
		}
	}
	st := s.stats(t)
	if st.OriginRequests != 3 || st.OriginBytes != testSize || len(st.Slices[testPath]) != 3 {
		t.Errorf("stats %+v", st)
	}
	for idx, n := range st.Slices[testPath] {
		if n != 1 {
			t.Errorf("slice %d pulled %d times", idx, n)
		}
	}
}

func TestSlicesPulledOnce(t *testing.T) {
	s := newStand(t, modeGcoreMD5)
	s.origin.set(0, 50*time.Millisecond)
	url := s.signed(testPath)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a := get(t, url, "bytes=500-1500"); a.status != http.StatusPartialContent {
				t.Errorf("parallel: %d", a.status)
			}
		}()
	}
	wg.Wait()
	if n := len(s.origin.seen()); n != 2 {
		t.Errorf("origin requests %d, want 2", n)
	}
	s.ctlPost(t, "/_ctl/flush")
	get(t, url, "bytes=0-9")
	if n := len(s.origin.seen()); n != 3 {
		t.Errorf("after flush: origin requests %d, want 3", n)
	}
}

func TestSwitches(t *testing.T) {
	s := newStand(t, modeGcoreMD5)
	url := s.signed(testPath)
	want := s.origin.content

	s.ctlPost(t, "/_ctl/tamper?on=1")
	a := get(t, url, "bytes=100-199")
	if a.body[0] != want[100]^0xff || !bytes.Equal(a.body[1:], want[101:200]) {
		t.Error("tamper: not exactly the first byte flipped")
	}
	s.ctlPost(t, "/_ctl/tamper?on=1&offset=1500")
	if a = get(t, url, "bytes=1000-1999"); a.body[500] != want[1500]^0xff || !bytes.Equal(a.body[:500], want[1000:1500]) {
		t.Error("tamper offset: not exactly byte 1500 flipped")
	}
	if a = get(t, url, "bytes=0-99"); !bytes.Equal(a.body, want[:100]) {
		t.Error("tamper offset: a range without the offset changed")
	}
	s.ctlPost(t, "/_ctl/tamper?on=0")
	if a = get(t, url, "bytes=1000-1999"); !bytes.Equal(a.body, want[1000:2000]) {
		t.Error("tamper off: the cache kept the flipped byte")
	}

	s.ctlPost(t, "/_ctl/badrange?on=1")
	if a = get(t, url, "bytes=0-9"); a.status != 206 || a.header.Get("Content-Range") != "bytes 16-25/2500" || !bytes.Equal(a.body, want[:10]) {
		t.Errorf("badrange: %d %q", a.status, a.header.Get("Content-Range"))
	}
	s.ctlPost(t, "/_ctl/badrange?on=0")

	s.ctlPost(t, "/_ctl/full200?on=1")
	if a = get(t, url, "bytes=0-9"); a.status != 200 || !bytes.Equal(a.body, want) {
		t.Errorf("full200: %d, %d bytes", a.status, len(a.body))
	}
	s.ctlPost(t, "/_ctl/full200?on=0")

	s.ctlPost(t, "/_ctl/down?on=1")
	if a = get(t, url, "bytes=0-9"); a.status != http.StatusBadGateway {
		t.Errorf("down: %d", a.status)
	}
	s.ctlPost(t, "/_ctl/reset")
	if a = get(t, url, "bytes=0-9"); a.status != 206 {
		t.Errorf("after reset: %d", a.status)
	}
	if st := s.stats(t); st.ClientRequests != 1 || st.Responses[206] != 1 {
		t.Errorf("after reset: stats %+v", st)
	}

	// The control API is not on the CDN port.
	if a = get(t, s.cdn.URL+"/_ctl/stats", ""); a.status != http.StatusForbidden {
		t.Errorf("/_ctl/stats on the CDN port: %d", a.status)
	}
}

func TestOriginFailures(t *testing.T) {
	s := newStand(t, modeGcoreMD5)
	url := s.signed(testPath)
	s.origin.set(http.StatusNotFound, 0)
	if a := get(t, url, "bytes=0-9"); a.status != http.StatusNotFound {
		t.Errorf("origin 404: %d", a.status)
	}
	s.origin.set(http.StatusOK, 0)
	if a := get(t, url, "bytes=0-9"); a.status != http.StatusBadGateway {
		t.Errorf("origin 200 to a slice: %d", a.status)
	}
	s.origin.set(0, 0)
	s.origin.mu.Lock()
	s.origin.wantAuth = "another value"
	s.origin.mu.Unlock()
	if a := get(t, url, "bytes=0-9"); a.status != http.StatusForbidden {
		t.Errorf("origin refusing the header: %d", a.status)
	}
}

// TestPathHMAC: a caching proxy by the full path that keeps a slice for the
// origin's max-age and passes the origin's refusals on.
func TestPathHMAC(t *testing.T) {
	s := newStand(t, modePathHMAC)
	s.origin.cacheControl = "public, max-age=60"
	path := "/t/1794863600/AAAAAAAAAAAAAAAAAAAAAA" + testPath
	if a := get(t, s.cdn.URL+path, "bytes=0-9"); a.status != 206 || a.header.Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("path-hmac: %d %v", a.status, a.header)
	}
	get(t, s.cdn.URL+path, "bytes=10-19")
	if n := len(s.origin.seen()); n != 1 {
		t.Errorf("within max-age: origin requests %d, want 1", n)
	}
	other := strings.Replace(path, "/t/1794863600/", "/t/1794867200/", 1)
	get(t, s.cdn.URL+other, "bytes=0-9")
	if n := len(s.origin.seen()); n != 2 {
		t.Errorf("another path: origin requests %d, want 2", n)
	}
	s.advance(61 * time.Second)
	get(t, s.cdn.URL+path, "bytes=0-9")
	if n := len(s.origin.seen()); n != 3 {
		t.Errorf("after max-age: origin requests %d, want 3", n)
	}
	// The slice pulled again replaced the old one: nothing outside the bound.
	s.c.mu.Lock()
	order, cached := len(s.c.order), s.c.cached
	s.c.mu.Unlock()
	if order != 2 || cached != 2*testSlice {
		t.Errorf("after a refresh: %d slices in the order, %d bytes cached, want 2 and %d", order, cached, 2*testSlice)
	}
	s.origin.set(http.StatusGone, 0)
	if a := get(t, s.cdn.URL+path+"x", "bytes=0-9"); a.status != http.StatusGone {
		t.Errorf("origin 410: %d", a.status)
	}
}

func TestMaxAge(t *testing.T) {
	for cc, want := range map[string]time.Duration{
		"public, max-age=2592000, immutable": 2592000 * time.Second,
		"public, max-age=60":                 time.Minute,
		"no-store":                           0,
		"private, max-age=60":                0,
		"":                                   0,
		"max-age=x":                          0,
	} {
		if got := maxAge(cc); got != want {
			t.Errorf("maxAge(%q) = %v, want %v", cc, got, want)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	set := func(kv ...string) {
		for _, k := range []string{"FAKECDN_ORIGIN", "CDN_ORIGIN_AUTH", "CDN_GCORE_SIGNER", "CDN_GCORE_TOKEN_KEY", "FAKECDN_CACHE_MB"} {
			t.Setenv(k, "")
		}
		for i := 0; i < len(kv); i += 2 {
			t.Setenv(kv[i], kv[i+1])
		}
	}
	set("FAKECDN_ORIGIN", "http://bff:20480/", "CDN_ORIGIN_AUTH", testAuth, "CDN_GCORE_TOKEN_KEY", testKey)
	cfg, err := configFromEnv()
	if err != nil || cfg.origin != "http://bff:20480" || cfg.mode != modeGcoreMD5 || cfg.sliceSize != 10<<20 {
		t.Errorf("gcore-md5: %+v, %v", cfg, err)
	}
	set("FAKECDN_ORIGIN", "http://bff:20480", "CDN_ORIGIN_AUTH", testAuth, "CDN_GCORE_SIGNER", modePathHMAC)
	if _, err = configFromEnv(); err != nil {
		t.Errorf("path-hmac without a token key: %v", err)
	}
	for i, kv := range [][]string{
		{"CDN_ORIGIN_AUTH", testAuth, "CDN_GCORE_TOKEN_KEY", testKey},
		{"FAKECDN_ORIGIN", "http://bff:20480", "CDN_GCORE_TOKEN_KEY", testKey},
		{"FAKECDN_ORIGIN", "http://bff:20480", "CDN_ORIGIN_AUTH", testAuth},
		{"FAKECDN_ORIGIN", "http://bff:20480", "CDN_ORIGIN_AUTH", testAuth, "CDN_GCORE_SIGNER", "s3"},
		{"FAKECDN_ORIGIN", "http://bff:20480", "CDN_ORIGIN_AUTH", testAuth, "CDN_GCORE_TOKEN_KEY", testKey, "FAKECDN_CACHE_MB", "0"},
	} {
		set(kv...)
		if _, err = configFromEnv(); err == nil {
			t.Errorf("case %d: no error", i)
		}
	}
}

// TestEviction: the cache stays within its bound, oldest slices first.
func TestEviction(t *testing.T) {
	s := newStand(t, modeGcoreMD5)
	s.c.cfg.cacheBytes = 2 * testSlice
	url := s.signed(testPath)
	get(t, url, "") // three slices, the first one dropped
	s.c.mu.Lock()
	cached, n := s.c.cached, len(s.c.cache)
	s.c.mu.Unlock()
	if cached > 2*testSlice || n != 2 {
		t.Errorf("cached %d bytes in %d slices", cached, n)
	}
	before := len(s.origin.seen())
	get(t, url, "bytes=0-9")
	if got := len(s.origin.seen()); got != before+1 {
		t.Errorf("the dropped slice: %d origin requests, want %d", got, before+1)
	}
}
