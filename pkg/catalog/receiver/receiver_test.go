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

package receiver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const token = "0123456789abcdef0123456789abcdef"

func newReceiver(t *testing.T) (*Receiver, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	r, err := New(root, token, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func do(r http.Handler, method, target, origin, tok string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if tok != "" {
		req.Header.Set("X-Run-Token", tok)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestReceiver(t *testing.T) {
	r, dir := newReceiver(t)

	pre := do(r, http.MethodOptions, "/collect/files/123.m", Origin, "", nil)
	if pre.Code != http.StatusNoContent || pre.Header().Get("Access-Control-Allow-Origin") != Origin ||
		!strings.Contains(pre.Header().Get("Access-Control-Allow-Headers"), "x-run-token") {
		t.Fatalf("preflight %d %v", pre.Code, pre.Header())
	}
	if w := do(r, http.MethodPut, "/collect/files/123.m", Origin, token, []byte("thumb")); w.Code != http.StatusNoContent {
		t.Fatalf("put %d", w.Code)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "files", "123.m")); err != nil || string(b) != "thumb" {
		t.Fatalf("written %q, %v", b, err)
	}
	if w := do(r, http.MethodHead, "/collect/files/123.m", Origin, token, nil); w.Code != http.StatusNoContent {
		t.Fatalf("head of a file there: %d", w.Code)
	}
	for _, p := range []string{"/collect/files/124.m", "/collect/files"} {
		if w := do(r, http.MethodHead, p, Origin, token, nil); w.Code == http.StatusNoContent {
			t.Fatalf("head of %s: %d", p, w.Code)
		}
	}
	if w := do(r, http.MethodHead, "/collect/files/123.m", Origin, "", nil); w.Code != http.StatusForbidden {
		t.Fatalf("head without the token: %d", w.Code)
	}
	if w := do(r, http.MethodPut, "/collect/manifest.json", Origin, token, []byte("{}")); w.Code != http.StatusNoContent {
		t.Fatalf("manifest %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", "123.m.part")); !os.IsNotExist(err) {
		t.Fatalf("a temporary file left: %v", err)
	}

	for name, c := range map[string]struct {
		method, target, origin, token string
		body                          []byte
		code                          int
	}{
		"another token":  {http.MethodPut, "/collect/files/1", Origin, strings.Repeat("x", 32), nil, http.StatusForbidden},
		"no token":       {http.MethodPut, "/collect/files/1", Origin, "", nil, http.StatusForbidden},
		"another origin": {http.MethodPut, "/collect/files/1", "https://evil.example", token, nil, http.StatusNotFound},
		"no origin":      {http.MethodPut, "/collect/files/1", "", token, nil, http.StatusNotFound},
		"outside":        {http.MethodPut, "/collect/../etc/passwd", Origin, token, nil, http.StatusBadRequest},
		"deeper":         {http.MethodPut, "/collect/files/a/b", Origin, token, nil, http.StatusBadRequest},
		"another folder": {http.MethodPut, "/collect/secrets/x", Origin, token, nil, http.StatusBadRequest},
		"dot":            {http.MethodPut, "/collect/files/..", Origin, token, nil, http.StatusBadRequest},
		"another path":   {http.MethodPut, "/files/1", Origin, token, nil, http.StatusNotFound},
		"another method": {http.MethodDelete, "/collect/files/1", Origin, token, nil, http.StatusMethodNotAllowed},
		"too large":      {http.MethodPut, "/collect/files/big", Origin, token, make([]byte, MaxFile+1), http.StatusRequestEntityTooLarge},
	} {
		if w := do(r, c.method, c.target, c.origin, c.token, c.body); w.Code != c.code {
			t.Errorf("%s: %d, want %d", name, w.Code, c.code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "files", "big")); !os.IsNotExist(err) {
		t.Fatalf("a file over the limit written: %v", err)
	}
	if _, err := New(nil, "short", t.Logf); err == nil {
		t.Fatal("a short token taken")
	}
}
