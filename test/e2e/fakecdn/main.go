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

// Command fakecdn plays the Gcore CDN resource of the HTTPS CDN (task 107,
// ADR 0071) for the end-to-end tests. Clients fetch the URLs of
// upload.fileCdnRedirect from it; it pulls the ciphertext from bff's origin
// as the resource does:
//
//   - gcore-md5 (CDN_GCORE_SIGNER, the default): every request carries the
//     Secure Token ?md5=<token>&expires=<unix>, checked here by its own
//     implementation of Gcore's formula, not by pkg/cdn, so that the two
//     cannot drift together: token = base64url(MD5(expires + path + " " +
//     key)) without padding, and a missing, wrong or expired token is 403
//     with no request to the origin
//     (https://docs.gcore.com/cdn/cdn-resource-options/security/use-a-secure-token/configure-and-use-secure-token.md,
//     read 2026-11-16). The cache key is the path without the query
//     ("Ignore query string").
//   - path-hmac: the signature is in the path and the origin checks it; this
//     is a caching proxy keyed by the full path.
//
// In both modes the origin is asked for slices of sliceSize bytes (Gcore's
// "Large files delivery optimization") with the header X-Pluma-Origin-Auth,
// the query string of the client as it came (the worst case for the origin)
// and no Accept-Encoding. A slice is kept in memory for the max-age of the
// origin's Cache-Control, and a client gets 206 with the exact Content-Range
// of any single range it asks for (200 without Range, 416 past the end).
//
// The tests steer it on a second port that the clients of the stand never
// use (FAKECDN_CONTROL_LISTEN); every switch is POST ...?on=1|0:
//
//	/_ctl/tamper   flip a byte of what is served: the first byte of every
//	               answer, or with &offset=N the byte at file offset N
//	               (the cache keeps the right bytes)
//	/_ctl/down     answer 502 to everything
//	/_ctl/badrange shift Content-Range of a 206 by 16 bytes
//	/_ctl/full200  answer a Range request with 200 and the whole file
//	/_ctl/notoken  serve gcore-md5 requests without checking the token
//	/_ctl/flush    (POST) forget the cached slices
//	/_ctl/reset    (POST) all switches off, cache and counters cleared
//	/_ctl/stats    (GET) the counters: requests and answers of the clients,
//	               requests and bytes to the origin, pulls of every slice
package main

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// originAuthHeader carries CDN_ORIGIN_AUTH to the origin, as the
	// request header the CDN resource is configured with.
	originAuthHeader = "X-Pluma-Origin-Auth"
	// defaultSliceSize is the slice of Gcore's large files delivery.
	defaultSliceSize = 10 << 20
	// originTimeout bounds one slice from the origin.
	originTimeout = time.Minute

	modeGcoreMD5 = "gcore-md5"
	modePathHMAC = "path-hmac"
)

type config struct {
	origin     string // the origin's base URL, http://bff:20480 in the stand
	originAuth string
	mode       string
	tokenKey   string // gcore-md5 only
	sliceSize  int64
	cacheBytes int64
}

// switches are what the tests turn on and off.
type switches struct {
	tamper       bool
	tamperOffset int64 // < 0: the first byte of every answer
	down         bool
	badrange     bool
	full200      bool
	notoken      bool
}

type sliceKey struct {
	path string
	idx  int64
}

// slice is one cached piece of an object, with the origin's headers.
type slice struct {
	key          sliceKey
	data         []byte
	total        int64
	etag         string
	lastModified string
	cacheControl string
	contentType  string
	expires      time.Time
}

// pull is the outcome of asking for a slice: the slice, or the origin's
// status to pass on (416 with the size it named), or a failure (502).
type pull struct {
	s      *slice
	status int
	total  int64
	err    error
}

type flight struct {
	done chan struct{}
	res  pull
}

type stats struct {
	ClientRequests int64                      `json:"client_requests"`
	Responses      map[int]int64              `json:"responses"`
	OriginRequests int64                      `json:"origin_requests"`
	OriginBytes    int64                      `json:"origin_bytes"`
	Slices         map[string]map[int64]int64 `json:"slices"`
}

func newStats() stats {
	return stats{Responses: map[int]int64{}, Slices: map[string]map[int64]int64{}}
}

type cdn struct {
	cfg    config
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	sw       switches
	cache    map[sliceKey]*slice
	order    []*slice // insertion order, for eviction
	cached   int64
	inflight map[sliceKey]*flight
	st       stats
}

func newCDN(cfg config) *cdn {
	return &cdn{
		cfg: cfg,
		// No Accept-Encoding: the transport would add gzip by itself.
		client:   &http.Client{Transport: &http.Transport{DisableCompression: true}},
		now:      time.Now,
		sw:       switches{tamperOffset: -1},
		cache:    map[sliceKey]*slice{},
		inflight: map[sliceKey]*flight{},
		st:       newStats(),
	}
}

// gcoreToken is Gcore's Secure Token without IP binding.
func gcoreToken(expires, path, key string) string {
	sum := md5.Sum([]byte(expires + path + " " + key))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *cdn) tokenValid(r *http.Request) bool {
	q := r.URL.Query()
	token, expires := q.Get("md5"), q.Get("expires")
	exp, err := strconv.ParseInt(expires, 10, 64)
	if token == "" || err != nil || exp < c.now().Unix() {
		return false
	}
	want := gcoreToken(expires, r.URL.Path, c.cfg.tokenKey)
	return subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (c *cdn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &recorder{ResponseWriter: w}
	defer func() {
		c.mu.Lock()
		c.st.ClientRequests++
		c.st.Responses[rec.status]++
		c.mu.Unlock()
	}()
	c.serve(rec, r)
}

func (c *cdn) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	c.mu.Lock()
	sw := c.sw
	c.mu.Unlock()
	if sw.down {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if c.cfg.mode == modeGcoreMD5 && !sw.notoken && !c.tokenValid(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	path := r.URL.EscapedPath()
	spec, ranged := parseRange(r.Header.Get("Range"))
	if sw.full200 {
		ranged = false
	}
	first := int64(0)
	if ranged && !spec.suffix {
		first = spec.start / c.cfg.sliceSize
	}
	got := map[int64]*slice{}
	res := c.slice(path, r.URL.RawQuery, first)
	switch {
	case res.err != nil:
		w.WriteHeader(http.StatusBadGateway)
		return
	case res.status == http.StatusRequestedRangeNotSatisfiable:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", res.total))
		w.WriteHeader(res.status)
		return
	case res.s == nil:
		w.WriteHeader(res.status)
		return
	}
	meta := res.s
	got[first] = meta
	total := meta.total

	start, end, status := int64(0), total-1, http.StatusOK
	if ranged {
		switch {
		case spec.suffix:
			start = max(total-spec.length, 0)
		case spec.start >= total:
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		default:
			start = spec.start
			if spec.end >= 0 && spec.end < end {
				end = spec.end
			}
		}
		status = http.StatusPartialContent
	}

	h := w.Header()
	h.Set("Content-Type", meta.contentType)
	h.Set("Accept-Ranges", "bytes")
	for name, v := range map[string]string{"ETag": meta.etag, "Last-Modified": meta.lastModified, "Cache-Control": meta.cacheControl} {
		if v != "" {
			h.Set(name, v)
		}
	}
	h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if status == http.StatusPartialContent {
		shift := int64(0)
		if sw.badrange {
			shift = 16
		}
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start+shift, end+shift, total))
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead || total == 0 {
		return
	}

	for idx := start / c.cfg.sliceSize; idx <= end/c.cfg.sliceSize; idx++ {
		s := got[idx]
		if s == nil {
			res := c.slice(path, r.URL.RawQuery, idx)
			if res.s == nil || res.s.total != total || res.s.etag != meta.etag {
				// The headers are out: only a cut connection tells the
				// client, as a CDN does when the origin fails mid-file.
				panic(http.ErrAbortHandler)
			}
			s = res.s
		}
		base := idx * c.cfg.sliceSize
		lo, hi := max(start, base)-base, min(end+1, base+int64(len(s.data)))-base
		part := s.data[lo:hi]
		if sw.tamper {
			at := start
			if sw.tamperOffset >= 0 {
				at = sw.tamperOffset
			}
			if at >= base+lo && at < base+hi {
				part = append([]byte(nil), part...)
				part[at-base-lo] ^= 0xff
			}
		}
		if _, err := w.Write(part); err != nil {
			return
		}
	}
}

// rangeSpec is one range of a Range header: start-end (end < 0: open) or
// the last length bytes.
type rangeSpec struct {
	start, end int64
	suffix     bool
	length     int64
}

// parseRange reads a single byte range. Anything else, several ranges
// included, is ignored as RFC 9110 §14.2 allows: the answer is the whole
// file.
func parseRange(v string) (rangeSpec, bool) {
	spec, ok := strings.CutPrefix(v, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return rangeSpec{}, false
	}
	a, b, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return rangeSpec{}, false
	}
	if a == "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return rangeSpec{}, false
		}
		return rangeSpec{suffix: true, length: n}, true
	}
	start, err := strconv.ParseInt(a, 10, 64)
	if err != nil || start < 0 {
		return rangeSpec{}, false
	}
	end := int64(-1)
	if b != "" {
		if end, err = strconv.ParseInt(b, 10, 64); err != nil || end < start {
			return rangeSpec{}, false
		}
	}
	return rangeSpec{start: start, end: end}, true
}

// slice returns slice idx of path from the cache or, once for all the
// requests that want it at the same time, from the origin.
func (c *cdn) slice(path, query string, idx int64) pull {
	k := sliceKey{path, idx}
	c.mu.Lock()
	if s, ok := c.cache[k]; ok && c.now().Before(s.expires) {
		c.mu.Unlock()
		return pull{s: s}
	}
	if f, ok := c.inflight[k]; ok {
		c.mu.Unlock()
		<-f.done
		return f.res
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[k] = f
	c.mu.Unlock()

	f.res = c.fetch(k, query)

	c.mu.Lock()
	delete(c.inflight, k)
	if s := f.res.s; s != nil && c.now().Before(s.expires) {
		c.store(s)
	}
	c.mu.Unlock()
	close(f.done)
	return f.res
}

// store caches s, dropping the oldest slices over the bound. c.mu is held.
func (c *cdn) store(s *slice) {
	if old, ok := c.cache[s.key]; ok {
		// A slice pulled again after its max-age: the old one leaves the
		// order too, or it would stay in memory outside the bound.
		c.cached -= int64(len(old.data))
		c.order = slices.DeleteFunc(c.order, func(o *slice) bool { return o == old })
	}
	c.cache[s.key] = s
	c.order = append(c.order, s)
	c.cached += int64(len(s.data))
	for c.cached > c.cfg.cacheBytes && len(c.order) > 0 {
		old := c.order[0]
		c.order = c.order[1:]
		if c.cache[old.key] == old {
			delete(c.cache, old.key)
			c.cached -= int64(len(old.data))
		}
	}
}

func (c *cdn) fetch(k sliceKey, query string) pull {
	ctx, cancel := context.WithTimeout(context.Background(), originTimeout)
	defer cancel()
	u := c.cfg.origin + k.path
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return pull{err: err}
	}
	from := k.idx * c.cfg.sliceSize
	req.Header.Set(originAuthHeader, c.cfg.originAuth)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, from+c.cfg.sliceSize-1))

	c.mu.Lock()
	c.st.OriginRequests++
	if c.st.Slices[k.path] == nil {
		c.st.Slices[k.path] = map[int64]int64{}
	}
	c.st.Slices[k.path][k.idx]++
	c.mu.Unlock()

	resp, err := c.client.Do(req)
	if err != nil {
		return pull{err: err}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusRequestedRangeNotSatisfiable:
		var total int64
		if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes */%d", &total); err != nil {
			return pull{err: fmt.Errorf("416 without the size: %w", err)}
		}
		return pull{status: resp.StatusCode, total: total}
	case http.StatusOK:
		// A slice must be a 206: a 200 to a Range is a broken origin.
		return pull{err: errors.New("origin answered a slice with 200")}
	default:
		return pull{status: resp.StatusCode}
	}

	var a, b, total int64
	if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &a, &b, &total); err != nil {
		return pull{err: fmt.Errorf("content range of a slice: %w", err)}
	}
	if a != from || total <= a || b != min(from+c.cfg.sliceSize, total)-1 {
		return pull{err: fmt.Errorf("origin sent bytes %d-%d/%d for slice %d", a, b, total, k.idx)}
	}
	data := make([]byte, b-a+1)
	if _, err := io.ReadFull(resp.Body, data); err != nil {
		return pull{err: err}
	}
	if n, _ := resp.Body.Read(make([]byte, 1)); n != 0 {
		return pull{err: errors.New("origin sent more than its Content-Range")}
	}
	c.mu.Lock()
	c.st.OriginBytes += int64(len(data))
	c.mu.Unlock()

	cc := resp.Header.Get("Cache-Control")
	return pull{s: &slice{
		key:          k,
		data:         data,
		total:        total,
		etag:         resp.Header.Get("ETag"),
		lastModified: resp.Header.Get("Last-Modified"),
		cacheControl: cc,
		contentType:  resp.Header.Get("Content-Type"),
		expires:      c.now().Add(maxAge(cc)),
	}}
}

// maxAge is how long a CDN may keep an answer by its Cache-Control: 0
// without max-age or with no-store, no-cache or private.
func maxAge(cc string) time.Duration {
	var age time.Duration
	for _, d := range strings.Split(cc, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		switch {
		case d == "no-store" || d == "no-cache" || d == "private":
			return 0
		case strings.HasPrefix(d, "max-age="):
			n, err := strconv.ParseInt(d[len("max-age="):], 10, 64)
			if err != nil || n < 0 {
				return 0
			}
			age = time.Duration(n) * time.Second
		}
	}
	return age
}

func (c *cdn) control() http.Handler {
	mux := http.NewServeMux()
	toggle := func(name string, set func(*switches, bool, *http.Request) error) {
		mux.HandleFunc("POST /_ctl/"+name, func(w http.ResponseWriter, r *http.Request) {
			on, err := strconv.ParseBool(r.URL.Query().Get("on"))
			if err != nil {
				http.Error(w, "on=1 or on=0", http.StatusBadRequest)
				return
			}
			c.mu.Lock()
			err = set(&c.sw, on, r)
			c.mu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	toggle("tamper", func(s *switches, on bool, r *http.Request) error {
		s.tamper, s.tamperOffset = on, -1
		if v := r.URL.Query().Get("offset"); on && v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return errors.New("offset: a file offset")
			}
			s.tamperOffset = n
		}
		return nil
	})
	toggle("down", func(s *switches, on bool, _ *http.Request) error { s.down = on; return nil })
	toggle("badrange", func(s *switches, on bool, _ *http.Request) error { s.badrange = on; return nil })
	toggle("full200", func(s *switches, on bool, _ *http.Request) error { s.full200 = on; return nil })
	toggle("notoken", func(s *switches, on bool, _ *http.Request) error { s.notoken = on; return nil })
	mux.HandleFunc("POST /_ctl/flush", func(w http.ResponseWriter, _ *http.Request) {
		c.mu.Lock()
		c.flush()
		c.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /_ctl/reset", func(w http.ResponseWriter, _ *http.Request) {
		c.mu.Lock()
		c.flush()
		c.sw = switches{tamperOffset: -1}
		c.st = newStats()
		c.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /_ctl/stats", func(w http.ResponseWriter, _ *http.Request) {
		c.mu.Lock()
		body, err := json.Marshal(c.st)
		c.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	return mux
}

// flush forgets the cache. c.mu is held.
func (c *cdn) flush() {
	c.cache = map[sliceKey]*slice{}
	c.order = nil
	c.cached = 0
}

func configFromEnv() (config, error) {
	cfg := config{
		origin:     strings.TrimRight(os.Getenv("FAKECDN_ORIGIN"), "/"),
		originAuth: os.Getenv("CDN_ORIGIN_AUTH"),
		mode:       os.Getenv("CDN_GCORE_SIGNER"),
		tokenKey:   os.Getenv("CDN_GCORE_TOKEN_KEY"),
		sliceSize:  defaultSliceSize,
		cacheBytes: 512 << 20,
	}
	if cfg.mode == "" {
		cfg.mode = modeGcoreMD5
	}
	if v := os.Getenv("FAKECDN_CACHE_MB"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return cfg, errors.New("FAKECDN_CACHE_MB: a positive number of MiB")
		}
		cfg.cacheBytes = n << 20
	}
	switch {
	case cfg.origin == "":
		return cfg, errors.New("FAKECDN_ORIGIN is not set")
	case cfg.originAuth == "":
		return cfg, errors.New("CDN_ORIGIN_AUTH is not set")
	case cfg.mode != modeGcoreMD5 && cfg.mode != modePathHMAC:
		return cfg, fmt.Errorf("CDN_GCORE_SIGNER: %s or %s", modeGcoreMD5, modePathHMAC)
	case cfg.mode == modeGcoreMD5 && cfg.tokenKey == "":
		return cfg, errors.New("CDN_GCORE_TOKEN_KEY is not set")
	}
	return cfg, nil
}

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	cfg, err := configFromEnv()
	if err != nil {
		log.Fatalf("fakecdn: %v", err)
	}
	c := newCDN(cfg)
	ctl := &http.Server{Addr: getenv("FAKECDN_CONTROL_LISTEN", ":8081"), Handler: c.control(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Fatal(ctl.ListenAndServe())
	}()
	srv := &http.Server{Addr: getenv("FAKECDN_LISTEN", ":8080"), Handler: c, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 2 * time.Minute}
	log.Printf("fakecdn: %s, CDN on %s, control on %s", cfg.mode, srv.Addr, ctl.Addr)
	log.Fatal(srv.ListenAndServe())
}
