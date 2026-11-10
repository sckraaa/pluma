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

// Command fakecodes plays app/ops/codes, the Telegram account that writes
// login codes, for the end-to-end tests (task 076). bff sends it codes as to
// the real service (POST /v1/send with the bearer token); a phone gets the
// result a test chose for it, "no_telegram" by default, so the other tests
// log in through the administrator as without a sender. The tests choose the
// result and read the codes "written" on a loopback port:
//
//	POST /e2e/result?phone=...&result=sent   what /v1/send answers for the phone
//	GET  /e2e/codes?phone=...                the codes it took, oldest first
package main

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// account is the username of the account, given with an accepted code.
const account = "pluma_codes"

type stub struct {
	token   string
	mu      sync.Mutex
	results map[string]string
	codes   map[string][]string
}

func (s *stub) send(w http.ResponseWriter, r *http.Request) {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Phone  string `json:"phone"`
		Code   string `json:"code"`
		WaitMs int64  `json:"wait_ms"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&req); err != nil || req.Phone == "" || req.Code == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	result, ok := s.results[req.Phone]
	if !ok {
		result = "no_telegram"
	}
	resp := map[string]string{"result": result}
	if result == "sent" || result == "queued" {
		s.codes[req.Phone] = append(s.codes[req.Phone], req.Code)
		resp["account"] = account
	}
	s.mu.Unlock()
	writeJSON(w, resp)
}

func (s *stub) setResult(w http.ResponseWriter, r *http.Request) {
	phone, result := r.URL.Query().Get("phone"), r.URL.Query().Get("result")
	if phone == "" || result == "" {
		http.Error(w, "phone and result", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.results[phone] = result
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) listCodes(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	codes := append([]string{}, s.codes[r.URL.Query().Get("phone")]...)
	s.mu.Unlock()
	writeJSON(w, codes)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("fakecodes: write: %v", err)
	}
}

func main() {
	token := os.Getenv("CODES_API_TOKEN")
	if token == "" {
		log.Fatal("fakecodes: CODES_API_TOKEN is not set")
	}
	s := &stub{token: token, results: map[string]string{}, codes: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/send", s.send)
	mux.HandleFunc("POST /e2e/result", s.setResult)
	mux.HandleFunc("GET /e2e/codes", s.listCodes)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
