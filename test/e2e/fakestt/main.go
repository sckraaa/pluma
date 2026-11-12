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

// Command fakestt plays the speech engine of voice transcription (app/ops/stt
// or any service with the transcription endpoint of the OpenAI API) for the
// end-to-end tests (task 090). bff sends it sound as to the real one: POST
// /v1/audio/transcriptions, a multipart form with the bearer token. It
// checks what a real engine relies on (the model name, a WAV file that is
// PCM 16 bit, 16 kHz, mono) and answers what a test planned. The tests plan
// and look on a loopback port:
//
//	POST /e2e/plan   {"text":"...","delay_ms":0,"fail":0,"status":500,"hang":false}
//	                 the answers from now on: fail answers with status first
//	                 (-1: always), then text after delay_ms; hang holds the
//	                 request until the caller gives up. It zeroes the counter.
//	GET  /e2e/calls  {"calls":N,"seconds":S,"model":"...","language":"..."}:
//	                 the requests that carried sound since the plan, and what
//	                 the last one was
package main

import (
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	sampleRate = 16000
	// headLen is the start of the file that holds the WAV header; the rest
	// is counted, not kept: a record of any length goes through.
	headLen  = 4096
	maxField = 4096
)

type plan struct {
	Text    string `json:"text"`
	DelayMs int    `json:"delay_ms"`
	// Fail is how many requests fail first; -1: all of them.
	Fail   int  `json:"fail"`
	Status int  `json:"status"`
	Hang   bool `json:"hang"`
}

type seen struct {
	Calls    int     `json:"calls"`
	Seconds  float64 `json:"seconds"`
	Model    string  `json:"model"`
	Language string  `json:"language"`
}

type stub struct {
	token, model string
	mu           sync.Mutex
	plan         plan
	seen         seen
}

// soundSeconds checks that wav, the first bytes of a file of total bytes,
// is RIFF/WAVE with 16 kHz mono 16-bit PCM and returns how long it is.
func soundSeconds(wav []byte, total int64) (float64, error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return 0, errors.New("not RIFF/WAVE")
	}
	format := false
	for rest := wav[12:]; len(rest) >= 8; {
		id, size := string(rest[0:4]), int(binary.LittleEndian.Uint32(rest[4:8]))
		rest = rest[8:]
		if id == "data" {
			if !format {
				return 0, errors.New("samples before the format")
			}
			// The samples run to the end of the file, or to the size of
			// the chunk if that is less.
			samples := total - int64(len(wav)-len(rest))
			if int64(size) < samples {
				samples = int64(size)
			}
			return float64(samples) / (sampleRate * 2), nil
		}
		if size > len(rest) {
			return 0, errors.New("a chunk runs past the end")
		}
		if id == "fmt " {
			if size < 16 {
				return 0, errors.New("short format")
			}
			f := rest[:size]
			if binary.LittleEndian.Uint16(f[0:2]) != 1 || binary.LittleEndian.Uint16(f[2:4]) != 1 ||
				binary.LittleEndian.Uint32(f[4:8]) != sampleRate || binary.LittleEndian.Uint16(f[14:16]) != 16 {
				return 0, errors.New("not 16 kHz mono 16-bit PCM")
			}
			format = true
		}
		rest = rest[size+size%2:]
	}
	return 0, errors.New("no samples")
}

func (s *stub) transcribe(w http.ResponseWriter, r *http.Request) {
	if s.token != "" {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	form, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "not a multipart form", http.StatusBadRequest)
		return
	}
	var (
		fields  = map[string]string{}
		seconds float64
		heard   bool
	)
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "not a multipart form", http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			value, err := io.ReadAll(io.LimitReader(part, maxField))
			if err != nil {
				http.Error(w, "short field", http.StatusBadRequest)
				return
			}
			fields[part.FormName()] = string(value)
			continue
		}
		head := make([]byte, headLen)
		n, err := io.ReadFull(part, head)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			http.Error(w, "short file", http.StatusBadRequest)
			return
		}
		rest, err := io.Copy(io.Discard, part)
		if err != nil {
			http.Error(w, "short file", http.StatusBadRequest)
			return
		}
		if seconds, err = soundSeconds(head[:n], int64(n)+rest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		heard = true
	}
	if fields["model"] != s.model {
		http.Error(w, "unknown model", http.StatusNotFound)
		return
	}
	if f := fields["response_format"]; f != "" && f != "json" {
		http.Error(w, "response_format", http.StatusBadRequest)
		return
	}
	if !heard {
		http.Error(w, "no file", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.seen.Calls++
	s.seen.Seconds, s.seen.Model, s.seen.Language = seconds, fields["model"], fields["language"]
	p := s.plan
	fail := p.Fail != 0
	if p.Fail > 0 {
		s.plan.Fail--
	}
	s.mu.Unlock()

	if fail {
		status := p.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		http.Error(w, "planned failure", status)
		return
	}
	if p.Hang {
		<-r.Context().Done()
		return
	}
	select {
	case <-time.After(time.Duration(p.DelayMs) * time.Millisecond):
	case <-r.Context().Done():
		return
	}
	writeJSON(w, map[string]string{"text": p.Text})
}

func (s *stub) setPlan(w http.ResponseWriter, r *http.Request) {
	var p plan
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		http.Error(w, "bad plan", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.plan, s.seen = p, seen{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) calls(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	v := s.seen
	s.mu.Unlock()
	writeJSON(w, v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("fakestt: write: %v", err)
	}
}

func main() {
	model := os.Getenv("STT_MODEL")
	if model == "" {
		log.Fatal("fakestt: STT_MODEL is not set")
	}
	s := &stub{token: os.Getenv("STT_API_TOKEN"), model: model}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/audio/transcriptions", s.transcribe)
	mux.HandleFunc("POST /e2e/plan", s.setPlan)
	mux.HandleFunc("GET /e2e/calls", s.calls)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
