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

// Command fakemail plays Resend, the mail provider of the recovery email of
// the cloud password, for the end-to-end tests (task 091). bff sends it
// emails as to POST https://api.resend.com/emails: the bearer key and a
// User-Agent are required (Resend refuses a request without one), the same
// Idempotency-Key gives the same id without a second email. A test plans
// errors for an address and reads the emails "sent" on a loopback port:
//
//	POST /e2e/fail?to=...&status=503&name=service_unavailable&count=3
//	                                    the next count emails to the address get this error
//	GET  /e2e/emails?to=...             the emails accepted for the address, oldest first
package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type email struct {
	Id             string `json:"id"`
	From           string `json:"from"`
	To             string `json:"to"`
	Subject        string `json:"subject"`
	Text           string `json:"text"`
	HTML           string `json:"html"`
	ReplyTo        string `json:"reply_to,omitempty"`
	Tag            string `json:"tag"`
	IdempotencyKey string `json:"idempotency_key"`
}

type failure struct {
	status int
	name   string
}

type stub struct {
	key string

	mu     sync.Mutex
	seq    int
	emails map[string][]email   // by address
	byKey  map[string]string    // idempotency key: id
	fails  map[string][]failure // by address
}

func (s *stub) send(w http.ResponseWriter, r *http.Request) {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.key)) != 1 {
		resendError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	if r.Header.Get("User-Agent") == "" {
		resendError(w, http.StatusForbidden, "missing_user_agent")
		return
	}
	var req struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
		HTML    string   `json:"html"`
		ReplyTo string   `json:"reply_to"`
		Tags    []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil ||
		req.From == "" || len(req.To) != 1 || req.Subject == "" || req.Text == "" {
		resendError(w, http.StatusUnprocessableEntity, "validation_error")
		return
	}
	to := req.To[0]
	key := r.Header.Get("Idempotency-Key")

	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.fails[to]; len(f) > 0 {
		s.fails[to] = f[1:]
		resendError(w, f[0].status, f[0].name)
		return
	}
	if id, ok := s.byKey[key]; ok && key != "" {
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
		return
	}
	s.seq++
	e := email{
		Id: fmt.Sprintf("e2e-%06d", s.seq), From: req.From, To: to, Subject: req.Subject,
		Text: req.Text, HTML: req.HTML, ReplyTo: req.ReplyTo, IdempotencyKey: key,
	}
	for _, t := range req.Tags {
		if t.Name == "type" {
			e.Tag = t.Value
		}
	}
	s.emails[to] = append(s.emails[to], e)
	if key != "" {
		s.byKey[key] = e.Id
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": e.Id})
}

func (s *stub) fail(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status, err := strconv.Atoi(q.Get("status"))
	count, err2 := strconv.Atoi(q.Get("count"))
	if q.Get("to") == "" || err != nil || status < 400 || err2 != nil || count < 1 {
		http.Error(w, "to, status, count", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	for i := 0; i < count; i++ {
		s.fails[q.Get("to")] = append(s.fails[q.Get("to")], failure{status: status, name: q.Get("name")})
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) list(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	emails := append([]email{}, s.emails[r.URL.Query().Get("to")]...)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, emails)
}

// resendError answers as Resend does
// (https://resend.com/docs/api-reference/errors).
func resendError(w http.ResponseWriter, status int, name string) {
	body := map[string]any{"statusCode": status, "message": "fakemail"}
	if name != "" {
		body["name"] = name
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("fakemail: write: %v", err)
	}
}

func main() {
	key := os.Getenv("MAIL_API_KEY")
	if key == "" {
		log.Fatal("fakemail: MAIL_API_KEY is not set")
	}
	s := &stub{key: key, emails: map[string][]email{}, byKey: map[string]string{}, fails: map[string][]failure{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /emails", s.send)
	mux.HandleFunc("POST /e2e/fail", s.fail)
	mux.HandleFunc("GET /e2e/emails", s.list)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
