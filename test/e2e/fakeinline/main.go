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

// Command fakeinline plays the web for the built-in inline bots and the
// web files of the end-to-end tests (task 099, ADR 0062): the SearXNG of
// @pic and the sites whose pictures it finds and inline bots name. bff
// reaches it as fakeinline:8080, a host the stand's config lets it fetch
// from (WebFetch.AllowHosts, test/e2e/run.sh).
//
// The search answers as SearXNG's JSON format does
// (docs.searxng.org/dev/search_api.html, searx/webutils.py
// get_json_response), what app/bff/bots/internal/inline.Searxng reads:
//
//	GET /search?q=...&categories=images&format=json&pageno=N&...
//	                 results pictures a page (results=20 unless planned),
//	                 each with img_src and thumbnail_src on this host;
//	                 another format is 403, as an instance without json in
//	                 search.formats answers
//	GET /pic/{name}.jpg?w=W&h=H
//	                 a JPEG of W×H (320×240 by default, each side at most
//	                 2048), the same bytes for the same name and size: its
//	                 upper rows are noise from the name, so that a large
//	                 one takes several parts of upload.getWebFile
//
// The tests plan answers and read the requests on a loopback port; a
// search is planned by its text, a file by its path:
//
//	POST /e2e/plan?q=...|path=...&status=503&delay_ms=0&count=1[&results=N][&unresponsive=1]
//	                 the next count requests wait delay_ms and answer
//	                 status; a search answered 200 has N results, and with
//	                 unresponsive=1 names an engine that failed
//	GET  /e2e/requests?q=...|path=...
//	                 the requests of the text or the path, oldest first
//	                 (the last maxRequests of all are kept); without
//	                 either, all of them
//
// Photon (@places) has no part here: the places are not served (the
// owner's decision of 2026-11-16 in docs/tasks/099-inline-search-bots.md).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	// defaultResults is the pictures of a search page that is not planned
	// otherwise: more than a page of @pic (12), so a query pages within one
	// page of the engine.
	defaultResults = 20
	// maxResults bounds the pictures of a planned page.
	maxResults = 200
	// defaultW, defaultH are the size of a picture that names none.
	defaultW, defaultH = 320, 240
	// maxSide bounds a side of a picture.
	maxSide = 2048
	// noiseRows is how many upper rows of a picture are noise.
	noiseRows = 1024
	// maxRequests bounds the log of requests.
	maxRequests = 4096
	// maxDelay bounds a planned delay.
	maxDelay = time.Minute
)

// request is one request bff made.
type request struct {
	Seq int `json:"seq"`
	// Kind is "search" or "file".
	Kind string `json:"kind"`
	// Q, PageNo, Language, SafeSearch, Categories and Format are the
	// parameters of a search.
	Q          string `json:"q,omitempty"`
	PageNo     string `json:"pageno,omitempty"`
	Language   string `json:"language,omitempty"`
	SafeSearch string `json:"safesearch,omitempty"`
	Categories string `json:"categories,omitempty"`
	Format     string `json:"format,omitempty"`
	// Path is the path of a file.
	Path      string `json:"path,omitempty"`
	UserAgent string `json:"user_agent"`
	Accept    string `json:"accept"`
	// Status is what fakeinline answered.
	Status int `json:"status"`
}

type answer struct {
	status       int
	delay        time.Duration
	results      int
	unresponsive bool
}

type stub struct {
	mu       sync.Mutex
	seq      int
	requests []request
	plans    map[string][]answer // by planKey
}

func searchKey(q string) string  { return "q\x00" + q }
func fileKey(path string) string { return "path\x00" + path }

// take records req and returns its answer: the next one planned for key,
// or 200 with the default results.
func (s *stub) take(key string, req request) answer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	req.Seq = s.seq
	a := answer{status: http.StatusOK, results: defaultResults}
	if p := s.plans[key]; len(p) > 0 {
		a = p[0]
		if s.plans[key] = p[1:]; len(s.plans[key]) == 0 {
			delete(s.plans, key)
		}
	}
	req.Status = a.status
	s.requests = append(s.requests, req)
	if over := len(s.requests) - maxRequests; over > 0 {
		s.requests = append([]request(nil), s.requests[over:]...)
	}
	return a
}

// wait sleeps the planned delay; false when the request went away first.
func wait(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-time.After(d):
		return true
	case <-r.Context().Done():
		return false
	}
}

// searchResult is a result of SearXNG's JSON format, of the image
// category (searx/templates/simple/result_templates/images.html).
type searchResult struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	ImgSrc       string `json:"img_src"`
	ThumbnailSrc string `json:"thumbnail_src"`
	Engine       string `json:"engine"`
	Category     string `json:"category"`
	Template     string `json:"template"`
	Resolution   string `json:"resolution"`
	ImgFormat    string `json:"img_format"`
}

type searchAnswer struct {
	Query               string         `json:"query"`
	NumberOfResults     int            `json:"number_of_results"`
	Results             []searchResult `json:"results"`
	Answers             []string       `json:"answers"`
	Corrections         []string       `json:"corrections"`
	Infoboxes           []string       `json:"infoboxes"`
	Suggestions         []string       `json:"suggestions"`
	UnresponsiveEngines [][]string     `json:"unresponsive_engines"`
}

// pictureName is the name of the i-th picture of a page of a text: the
// same for the same search, so a repeated page names the same pictures.
func pictureName(q, pageno string, i int) string {
	h := sha256.Sum256([]byte(q + "\x00" + pageno + "\x00" + strconv.Itoa(i)))
	return hex.EncodeToString(h[:8])
}

func (s *stub) search(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	req := request{
		Kind: "search", Q: v.Get("q"), PageNo: v.Get("pageno"), Language: v.Get("language"),
		SafeSearch: v.Get("safesearch"), Categories: v.Get("categories"), Format: v.Get("format"),
		UserAgent: r.UserAgent(), Accept: r.Header.Get("Accept"),
	}
	a := s.take(searchKey(req.Q), req)
	if !wait(r, a.delay) {
		return
	}
	if a.status != http.StatusOK {
		http.Error(w, http.StatusText(a.status), a.status)
		return
	}
	if req.Format != "json" {
		// SearXNG refuses a format not in search.formats this way.
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	pageno := req.PageNo
	if pageno == "" {
		pageno = "1"
	}
	base := "http://" + r.Host
	out := searchAnswer{
		Query: req.Q, NumberOfResults: 0, Results: []searchResult{}, Answers: []string{},
		Corrections: []string{}, Infoboxes: []string{}, Suggestions: []string{}, UnresponsiveEngines: [][]string{},
	}
	for i := 0; i < a.results; i++ {
		name := pictureName(req.Q, pageno, i)
		out.Results = append(out.Results, searchResult{
			URL:          base + "/page/" + name,
			Title:        fmt.Sprintf("Picture %d", i+1),
			ImgSrc:       base + "/pic/" + name + ".jpg",
			ThumbnailSrc: base + "/pic/" + name + ".jpg?w=160&h=120",
			Engine:       "fake",
			Category:     "images",
			Template:     "images.html",
			Resolution:   "320 x 240",
			ImgFormat:    "jpeg",
		})
	}
	if a.unresponsive {
		out.UnresponsiveEngines = append(out.UnresponsiveEngines, []string{"fake", "timeout"})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("fakeinline: write: %v", err)
	}
}

func (s *stub) picture(w http.ResponseWriter, r *http.Request) {
	req := request{Kind: "file", Path: r.URL.Path, UserAgent: r.UserAgent(), Accept: r.Header.Get("Accept")}
	a := s.take(fileKey(req.Path), req)
	if !wait(r, a.delay) {
		return
	}
	if a.status != http.StatusOK {
		http.Error(w, http.StatusText(a.status), a.status)
		return
	}
	width, height, ok := pictureSize(r.URL.Query())
	if !ok {
		http.Error(w, "w, h", http.StatusBadRequest)
		return
	}
	body, err := pictureJPEG(r.PathValue("name"), width, height)
	if err != nil {
		log.Printf("fakeinline: encode: %v", err)
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if _, err = w.Write(body); err != nil {
		log.Printf("fakeinline: write: %v", err)
	}
}

// pictureSize reads w and h, the default size without them.
func pictureSize(v url.Values) (int, int, bool) {
	width, height := defaultW, defaultH
	var err error
	if s := v.Get("w"); s != "" {
		if width, err = strconv.Atoi(s); err != nil {
			return 0, 0, false
		}
	}
	if s := v.Get("h"); s != "" {
		if height, err = strconv.Atoi(s); err != nil {
			return 0, 0, false
		}
	}
	if width < 1 || height < 1 || width > maxSide || height > maxSide {
		return 0, 0, false
	}
	return width, height, true
}

// pictureJPEG draws the picture of name: noise from the name in the upper
// rows, a plain color of the name below. The same name and size give the
// same bytes (image/jpeg is deterministic).
func pictureJPEG(name string, width, height int) ([]byte, error) {
	sum := sha256.Sum256([]byte(name))
	rng := rand.New(rand.NewPCG(binary.BigEndian.Uint64(sum[:8]), binary.BigEndian.Uint64(sum[8:16])))
	plain := color.NRGBA{R: sum[16], G: sum[17], B: sum[18], A: 0xff}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := plain
			if y < noiseRows {
				v := rng.Uint32()
				c = color.NRGBA{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16), A: 0xff}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// planKeyOf is the key of the text or the path a request of /e2e names.
func planKeyOf(v url.Values) (string, bool) {
	switch {
	case v.Has("q") && !v.Has("path"):
		return searchKey(v.Get("q")), true
	case v.Has("path") && !v.Has("q"):
		return fileKey(v.Get("path")), true
	}
	return "", false
}

func (s *stub) plan(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	key, ok := planKeyOf(v)
	status, err1 := strconv.Atoi(v.Get("status"))
	count, err2 := strconv.Atoi(v.Get("count"))
	delayMs, results := 0, defaultResults
	var err3, err4 error
	if s := v.Get("delay_ms"); s != "" {
		delayMs, err3 = strconv.Atoi(s)
	}
	if s := v.Get("results"); s != "" {
		results, err4 = strconv.Atoi(s)
	}
	delay := time.Duration(delayMs) * time.Millisecond
	if !ok || err1 != nil || err2 != nil || err3 != nil || err4 != nil ||
		status < 200 || status > 599 || count < 1 || count > 100 || delay < 0 || delay > maxDelay ||
		results < 0 || results > maxResults {
		http.Error(w, "q or path, status, count, delay_ms, results", http.StatusBadRequest)
		return
	}
	a := answer{status: status, delay: delay, results: results, unresponsive: v.Get("unresponsive") == "1"}
	s.mu.Lock()
	for i := 0; i < count; i++ {
		s.plans[key] = append(s.plans[key], a)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) list(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	key, filtered := "", v.Has("q") || v.Has("path")
	if filtered {
		var ok bool
		if key, ok = planKeyOf(v); !ok {
			http.Error(w, "q or path", http.StatusBadRequest)
			return
		}
	}
	out := []request{}
	s.mu.Lock()
	for _, req := range s.requests {
		k := searchKey(req.Q)
		if req.Kind == "file" {
			k = fileKey(req.Path)
		}
		if !filtered || k == key {
			out = append(out, req)
		}
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("fakeinline: write: %v", err)
	}
}

func newStub() *stub {
	return &stub{plans: map[string][]answer{}}
}

func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /pic/{name}", s.picture)
	mux.HandleFunc("POST /e2e/plan", s.plan)
	mux.HandleFunc("GET /e2e/requests", s.list)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

func main() {
	srv := &http.Server{Addr: ":8080", Handler: newStub().handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
