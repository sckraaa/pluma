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

// fakepush stands in for APNs and FCM in the end-to-end tests (task 009):
// it accepts what the push service sends, keeps it, and gives it to the
// tests. Standard library only, as its image is built on its own.
//
//	fakepush -listen :8080            serve
//	fakepush -keys DIR -url URL       write throwaway provider keys for a stand
//
// Endpoints: POST /token (OAuth of FCM), POST /v1/projects/P/messages:send
// (FCM), POST [/sandbox]/3/device/TOKEN (APNs), GET /requests?token=T (what
// came, oldest first). A token starting with "unregistered" gets the
// provider's "invalid token" answer.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Request is a push as the provider got it.
type Request struct {
	Provider string            `json:"provider"` // apns or fcm
	Token    string            `json:"token"`
	Sandbox  bool              `json:"sandbox,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"` // apns-*
	Body     json.RawMessage   `json:"body"`
	At       time.Time         `json:"at"`
}

type store struct {
	mu       sync.Mutex
	requests []Request
}

func (s *store) add(r Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.At = time.Now()
	s.requests = append(s.requests, r)
}

func (s *store) list(token string) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Request{}
	for _, r := range s.requests {
		if token == "" || r.Token == token {
			out = append(out, r)
		}
	}
	return out
}

func main() {
	listen := flag.String("listen", ":8080", "address to serve")
	keys := flag.String("keys", "", "write provider keys into this directory and exit")
	url := flag.String("url", "http://fakepush:8080", "base URL of this server, as the push service reaches it (with -keys)")
	flag.Parse()
	if *keys != "" {
		if err := writeKeys(*keys, *url); err != nil {
			log.Fatal(err)
		}
		return
	}
	s := &store{}
	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fake-access-token", "expires_in": 3600, "token_type": "Bearer"})
	})
	http.HandleFunc("/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Message struct {
				Token string `json:"token"`
			} `json:"message"`
		}
		if json.Unmarshal(body, &req) != nil || !strings.HasSuffix(r.URL.Path, "/messages:send") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.add(Request{Provider: "fcm", Token: req.Message.Token, Body: body})
		if strings.HasPrefix(req.Message.Token, "unregistered") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"name":"projects/fake/messages/1"}`)
	})
	apns := func(sandbox bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			token := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			headers := map[string]string{}
			for _, h := range []string{"apns-topic", "apns-push-type", "apns-priority", "apns-expiration"} {
				headers[h] = r.Header.Get(h)
			}
			s.add(Request{Provider: "apns", Token: token, Sandbox: sandbox, Headers: headers, Body: body})
			if strings.HasPrefix(token, "unregistered") {
				w.WriteHeader(http.StatusGone)
				_, _ = io.WriteString(w, `{"reason":"Unregistered"}`)
			}
		}
	}
	http.HandleFunc("/3/device/", apns(false))
	http.HandleFunc("/sandbox/3/device/", apns(true))
	http.HandleFunc("/requests", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(s.list(r.URL.Query().Get("token")))
	})
	srv := &http.Server{Addr: *listen, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

// writeKeys writes a throwaway APNs key and FCM service account key whose
// token_uri is this server.
func writeKeys(dir, url string) error {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "apns_key.p8"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return err
	}
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	if der, err = x509.MarshalPKCS8PrivateKey(rk); err != nil {
		return err
	}
	creds, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "pluma-e2e",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "push@pluma-e2e.iam.gserviceaccount.com",
		"token_uri":    url + "/token",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "fcm_credentials.json"), creds, 0o600)
}
