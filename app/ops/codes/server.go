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
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/teamgram/teamgram-server/app/ops/codes/internal/sender"
)

// maxWait bounds how long POST /v1/send holds the caller: the login request
// is waiting behind it.
const maxWait = 10 * time.Second

var (
	phoneDigits = regexp.MustCompile(`^[0-9]{7,15}$`)
	codeDigits  = regexp.MustCompile(`^[0-9]{4,10}$`)
)

// submitter is the queue, as the handler uses it.
type submitter interface {
	Submit(ctx context.Context, phone, code string, wait time.Duration) sender.Result
	State() sender.State
	Account() string
}

type sendRequest struct {
	Phone  string `json:"phone"` // digits, as users.phone
	Code   string `json:"code"`
	WaitMs int64  `json:"wait_ms"`
}

type sendResponse struct {
	Result sender.Result `json:"result"`
	// Account is the username of the account that writes the code, given
	// with a code on its way: the app's login screen opens the chat with it.
	Account string `json:"account,omitempty"`
}

// newHandler is the internal API. /v1/* needs the bearer token, the rest
// shows nothing private. The request body is never logged.
func newHandler(q submitter, token string) http.Handler {
	authorized := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/state", authorized(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, q.State())
	}))
	mux.HandleFunc("POST /v1/send", authorized(func(w http.ResponseWriter, r *http.Request) {
		var req sendRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !phoneDigits.MatchString(req.Phone) || !codeDigits.MatchString(req.Code) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		wait := time.Duration(req.WaitMs) * time.Millisecond
		if wait < 0 || wait > maxWait {
			wait = maxWait
		}
		resp := sendResponse{Result: q.Submit(r.Context(), req.Phone, req.Code, wait)}
		if resp.Result == sender.Sent || resp.Result == sender.Queued {
			resp.Account = q.Account()
		}
		writeJSON(w, resp)
	}))
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("codes: write a response: %v", err)
	}
}
