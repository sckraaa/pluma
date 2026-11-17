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

// Command fakemaps plays the maps service (tileserver-gl, task 100, ADR
// 0063) for the end-to-end tests. bff asks it for static maps as it asks the
// real one (pkg/staticmap.HTTPRenderer):
//
//	GET /styles/{style}/static/{lon},{lat},{zoom}/{w}x{h}[@{s}x].png
//
// and gets a PNG of exactly w·s × h·s pixels whose first pixel carries the
// number of the request (red, green, blue: its bits 16-23, 8-15, 0-7), so a
// test tells which render a client got. The rest of the image is noise in
// its upper rows, so that a map takes several parts of upload.getWebFile.
// What tileserver-gl refuses it refuses the same way: a style it does not
// serve and a scale above maxScaleFactor 4 are 404 (the latter as
// TestTileserverGLZoomAndScale of pkg/staticmap observes), a side above
// maxSize 2048 after the scale is 400 (teamgramd/deploy/maps/config.json).
//
// The tests plan answers for a point and read the requests on a loopback
// port; a point is its longitude and latitude printed with six decimals, as
// bff prints them:
//
//	POST /e2e/plan?lon=...&lat=...&status=503&delay_ms=0&count=1
//	                 the next count requests for the point wait delay_ms and
//	                 answer status (200: the map)
//	GET  /e2e/requests?lon=...&lat=...
//	                 the requests for the point, oldest first (the last
//	                 maxRequests of all points are kept); without a point,
//	                 all of them
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limits of the maps service (teamgramd/deploy/maps/config.json).
const (
	maxScaleFactor = 4
	maxSize        = 2048
)

const (
	// maxRequests bounds the log of requests.
	maxRequests = 4096
	// noiseRows is how many upper rows are noise: about 0.4 MB of a map of
	// 554 pixels wide (277 points at 2x), several parts of 128 KB.
	noiseRows = 256
	// maxDelay bounds a planned delay.
	maxDelay = time.Minute
)

// sizeRe is the last segment of a static map path as tileserver-gl reads it
// (src/serve_rendered.js of v5.7.0 also takes other formats; bff asks only
// for png).
var sizeRe = regexp.MustCompile(`^(\d+)x(\d+)(?:@(\d+)x)?\.png$`)

// request is one map request bff made.
type request struct {
	Seq   int     `json:"seq"`
	Style string  `json:"style"`
	Lon   float64 `json:"lon"`
	Lat   float64 `json:"lat"`
	Zoom  float64 `json:"zoom"`
	W     int     `json:"w"`
	H     int     `json:"h"`
	Scale int     `json:"scale"`
	// Status is what fakemaps answered.
	Status int `json:"status"`
}

type answer struct {
	status int
	delay  time.Duration
}

type stub struct {
	styles map[string]bool

	mu       sync.Mutex
	seq      int
	requests []request
	plans    map[string][]answer // by point
}

// pointKey names a point as bff prints it (%.6f).
func pointKey(lon, lat float64) string {
	return fmt.Sprintf("%.6f,%.6f", lon, lat)
}

func (s *stub) static(w http.ResponseWriter, r *http.Request) {
	style := r.PathValue("style")
	center := strings.Split(r.PathValue("center"), ",")
	size := sizeRe.FindStringSubmatch(r.PathValue("size"))
	if len(center) != 3 || size == nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	lon, err1 := strconv.ParseFloat(center[0], 64)
	lat, err2 := strconv.ParseFloat(center[1], 64)
	zoom, err3 := strconv.ParseFloat(center[2], 64)
	width, err4 := strconv.Atoi(size[1])
	height, err5 := strconv.Atoi(size[2])
	scale := 1
	var err6 error
	if size[3] != "" {
		scale, err6 = strconv.Atoi(size[3])
	}
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req := request{Style: style, Lon: lon, Lat: lat, Zoom: zoom, W: width, H: height, Scale: scale}

	s.mu.Lock()
	s.seq++
	req.Seq = s.seq
	a := answer{status: http.StatusOK}
	key := pointKey(lon, lat)
	if p := s.plans[key]; len(p) > 0 {
		a = p[0]
		if s.plans[key] = p[1:]; len(s.plans[key]) == 0 {
			delete(s.plans, key)
		}
	}
	switch {
	case !s.styles[style], scale < 1, scale > maxScaleFactor:
		a.status = http.StatusNotFound
	case width < 1, height < 1, width*scale > maxSize, height*scale > maxSize:
		a.status = http.StatusBadRequest
	}
	req.Status = a.status
	s.requests = append(s.requests, req)
	if over := len(s.requests) - maxRequests; over > 0 {
		s.requests = append([]request(nil), s.requests[over:]...)
	}
	s.mu.Unlock()

	if a.delay > 0 {
		select {
		case <-time.After(a.delay):
		case <-r.Context().Done():
			return
		}
	}
	if a.status != http.StatusOK {
		http.Error(w, http.StatusText(a.status), a.status)
		return
	}
	body, err := mapImage(req.Seq, width*scale, height*scale)
	if err != nil {
		log.Printf("fakemaps: encode: %v", err)
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if _, err = w.Write(body); err != nil {
		log.Printf("fakemaps: write: %v", err)
	}
}

// mapImage draws the map of request seq: its number in the first pixel,
// noise in the upper rows, a plain color below.
func mapImage(seq, width, height int) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	rng := rand.New(rand.NewPCG(uint64(seq), 0x6d617073)) //nolint:gosec // the same noise for the same request
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.NRGBA{R: 0xe8, G: 0xe4, B: 0xd8, A: 0xff}
			if y < noiseRows {
				v := rng.Uint32()
				c = color.NRGBA{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16), A: 0xff}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	img.SetNRGBA(0, 0, color.NRGBA{R: uint8(seq >> 16), G: uint8(seq >> 8), B: uint8(seq), A: 0xff})
	var b bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (s *stub) plan(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lon, err1 := strconv.ParseFloat(q.Get("lon"), 64)
	lat, err2 := strconv.ParseFloat(q.Get("lat"), 64)
	status, err3 := strconv.Atoi(q.Get("status"))
	count, err4 := strconv.Atoi(q.Get("count"))
	delayMs := 0
	var err5 error
	if v := q.Get("delay_ms"); v != "" {
		delayMs, err5 = strconv.Atoi(v)
	}
	delay := time.Duration(delayMs) * time.Millisecond
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil ||
		status < 200 || status > 599 || count < 1 || count > 100 || delay < 0 || delay > maxDelay {
		http.Error(w, "lon, lat, status, count, delay_ms", http.StatusBadRequest)
		return
	}
	key := pointKey(lon, lat)
	s.mu.Lock()
	for i := 0; i < count; i++ {
		s.plans[key] = append(s.plans[key], answer{status: status, delay: delay})
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	key := ""
	if q.Has("lon") || q.Has("lat") {
		lon, err1 := strconv.ParseFloat(q.Get("lon"), 64)
		lat, err2 := strconv.ParseFloat(q.Get("lat"), 64)
		if err1 != nil || err2 != nil {
			http.Error(w, "lon, lat", http.StatusBadRequest)
			return
		}
		key = pointKey(lon, lat)
	}
	out := []request{}
	s.mu.Lock()
	for _, req := range s.requests {
		if key == "" || pointKey(req.Lon, req.Lat) == key {
			out = append(out, req)
		}
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("fakemaps: write: %v", err)
	}
}

func newStub(styles string) *stub {
	s := &stub{styles: map[string]bool{}, plans: map[string][]answer{}}
	for _, v := range strings.Split(styles, ",") {
		if v = strings.TrimSpace(v); v != "" {
			s.styles[v] = true
		}
	}
	return s
}

func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /styles/{style}/static/{center}/{size}", s.static)
	mux.HandleFunc("POST /e2e/plan", s.plan)
	mux.HandleFunc("GET /e2e/requests", s.list)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

func main() {
	// The styles of the stand: MAPS_STYLES of bff, ru,en by default.
	styles := os.Getenv("MAPS_STYLES")
	if strings.TrimSpace(styles) == "" {
		styles = "ru,en"
	}
	srv := &http.Server{Addr: ":8080", Handler: newStub(styles).handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
