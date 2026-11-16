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

package translate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef"

func testRequest() Request {
	return Request{
		Prompt:        "Translate the following segment into Russian, without additional explanation.\n\nHello",
		Temperature:   0.7,
		TopP:          0.6,
		TopK:          20,
		RepeatPenalty: 1.05,
		MaxTokens:     71,
		Stop:          []string{"</target>"},
		Chars:         5,
	}
}

func okAnswer(content, finish string) string {
	b, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-1",
		"object":  "chat.completion",
		"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content}}},
	})
	return string(b)
}

func TestHTTPEngineRequest(t *testing.T) {
	var (
		mu                        sync.Mutex
		method, path, auth, ctype string
		body                      map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		method, path, auth, ctype = r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		body = nil
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body: %v", err)
		}
		_, _ = io.WriteString(w, okAnswer("Привет", "stop"))
	}))
	defer srv.Close()

	e := NewHTTPEngine(HTTPConfig{URL: srv.URL + "/", Token: testToken, Model: "hy-mt1.5-1.8b"})
	got, err := e.Translate(context.Background(), testRequest())
	if err != nil || got != "Привет" {
		t.Fatalf("Translate = %q, %v", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if method != http.MethodPost || path != "/v1/chat/completions" || auth != "Bearer "+testToken || ctype != "application/json" {
		t.Errorf("request %s %s auth %q type %q", method, path, auth, ctype)
	}
	want := map[string]any{
		"model":          "hy-mt1.5-1.8b",
		"messages":       []any{map[string]any{"role": "user", "content": testRequest().Prompt}},
		"stream":         false,
		"temperature":    0.7,
		"top_p":          0.6,
		"top_k":          float64(20),
		"repeat_penalty": 1.05,
		"max_tokens":     float64(71),
		"stop":           []any{"</target>"},
	}
	wb, _ := json.Marshal(want)
	gb, _ := json.Marshal(body)
	if string(wb) != string(gb) {
		t.Errorf("body\n got %s\nwant %s", gb, wb)
	}

	// Without a token no Authorization; without stop words no "stop".
	e = NewHTTPEngine(HTTPConfig{URL: srv.URL, Model: "m"})
	req := testRequest()
	req.Stop = nil
	mu.Unlock()
	_, err = e.Translate(context.Background(), req)
	mu.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Errorf("auth %q without a token", auth)
	}
	if _, ok := body["stop"]; ok {
		t.Errorf("stop sent: %v", body["stop"])
	}
}

func TestHTTPEngineErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   []error
		not    []error
	}{
		{"cut by max_tokens", 200, okAnswer("Прив", "length"), []error{ErrBadAnswer}, []error{ErrUnavailable}},
		{"busy", 503, `{"error":{"code":503,"message":"Loading model"}}`, []error{ErrUnavailable, ErrBusy}, []error{ErrRejected}},
		{"too many", 429, "", []error{ErrUnavailable, ErrBusy}, []error{ErrRejected}},
		{"bad request", 400, `{"error":{"code":400}}`, []error{ErrRejected}, []error{ErrUnavailable}},
		{"too large", 413, "", []error{ErrRejected}, []error{ErrUnavailable}},
		{"token", 401, "", []error{ErrUnavailable, ErrDown}, []error{ErrRejected}},
		{"path", 404, "", []error{ErrUnavailable, ErrDown}, []error{ErrRejected}},
		{"server error", 500, "", []error{ErrUnavailable}, []error{ErrRejected, ErrBusy, ErrDown}},
		{"cut answer", 200, okAnswer("Привет", "stop")[:30], []error{ErrUnavailable}, nil},
		{"not json", 200, "<html>proxy</html>", []error{ErrUnavailable}, nil},
		{"no choices", 200, `{"choices":[]}`, []error{ErrUnavailable}, nil},
		{"no content", 200, `{"choices":[{"message":{"role":"assistant","content":null},"finish_reason":"stop"}]}`, []error{ErrUnavailable}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			defer srv.Close()
			_, err := NewHTTPEngine(HTTPConfig{URL: srv.URL, Token: testToken}).Translate(context.Background(), testRequest())
			for _, w := range c.want {
				if !errors.Is(err, w) {
					t.Errorf("err %v, want %v", err, w)
				}
			}
			for _, n := range c.not {
				if errors.Is(err, n) {
					t.Errorf("err %v is %v", err, n)
				}
			}
		})
	}
}

func TestHTTPEngineDeadline(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	// The deadline of the engine: Timeout + TimeoutPerChar × Chars.
	e := NewHTTPEngine(HTTPConfig{URL: srv.URL, Timeout: 50 * time.Millisecond, TimeoutPerChar: 10 * time.Millisecond})
	start := time.Now()
	_, err := e.Translate(context.Background(), testRequest())
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err %v", err)
	}
	if d := time.Since(start); d < 100*time.Millisecond || d > 5*time.Second {
		t.Errorf("gave up after %v, want 50 ms + 5 × 10 ms", d)
	}

	// The caller's deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	e = NewHTTPEngine(HTTPConfig{URL: srv.URL})
	if _, err = e.Translate(ctx, testRequest()); !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err %v", err)
	}
}

func TestHTTPEngineDown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	e := NewHTTPEngine(HTTPConfig{URL: "http://" + addr})
	if _, err = e.Translate(context.Background(), testRequest()); !errors.Is(err, ErrDown) {
		t.Errorf("Translate: %v, want ErrDown", err)
	}
	if err = e.Health(context.Background()); !errors.Is(err, ErrDown) {
		t.Errorf("Health: %v, want ErrDown", err)
	}
}

func TestHTTPEngineHealth(t *testing.T) {
	var (
		mu         sync.Mutex
		status     = http.StatusServiceUnavailable
		path, auth string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	e := NewHTTPEngine(HTTPConfig{URL: srv.URL, Token: testToken})
	if err := e.Health(context.Background()); !errors.Is(err, ErrBusy) {
		t.Errorf("loading: %v", err)
	}
	mu.Lock()
	status = http.StatusOK
	mu.Unlock()
	if err := e.Health(context.Background()); err != nil {
		t.Errorf("up: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if path != "/health" || auth != "Bearer "+testToken {
		t.Errorf("path %q auth %q", path, auth)
	}
}
