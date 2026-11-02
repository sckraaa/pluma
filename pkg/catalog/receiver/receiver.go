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

// Package receiver takes a collection of the catalog from the collector in
// a tab of Telegram Web K (task 024, ADR 0028; docs/ops/catalog.md): files
// PUT under /collect/ with a token of the run, from that page's origin
// only, written into one directory; HEAD tells whether a file is there. It runs only while a collection goes.
package receiver

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync/atomic"
)

// Origin is the page the collector runs in.
const Origin = "https://web.telegram.org"

// MaxFile is the largest file accepted: a wallpaper or a video sticker is
// a few megabytes.
const MaxFile = 64 << 20

// MinToken is the shortest token accepted, in characters.
const MinToken = 32

// A path of the collection: what the collector writes (manifest.json,
// answers/…, sets/…, documents/…, wallpapers/…, files/…), nothing else.
var validPath = regexp.MustCompile(`^(manifest\.json|failed\.json|(answers|sets|documents|wallpapers|files)/[A-Za-z0-9._+-]{1,160})$`)

// Receiver writes what it is given into Root.
type Receiver struct {
	Root  *os.Root
	Token string
	Logf  func(format string, args ...any)

	files atomic.Int64
}

// New returns a receiver writing into root, for token.
func New(root *os.Root, token string, logf func(string, ...any)) (*Receiver, error) {
	if len(token) < MinToken {
		return nil, errors.New("receiver: the token is too short")
	}
	return &Receiver{Root: root, Token: token, Logf: logf}, nil
}

func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	name, ok := strings.CutPrefix(req.URL.Path, "/collect/")
	if !ok || req.Header.Get("Origin") != Origin {
		http.NotFound(w, req)
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", Origin)
	h.Set("Vary", "Origin")
	switch req.Method {
	case http.MethodOptions:
		h.Set("Access-Control-Allow-Methods", "PUT, HEAD")
		h.Set("Access-Control-Allow-Headers", "content-type, x-run-token")
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPut, http.MethodHead:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Header.Get("X-Run-Token")), []byte(r.Token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !validPath.MatchString(name) || path.Clean(name) != name {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	if req.Method == http.MethodHead {
		// Whether a file is here: a collection that stopped goes on from
		// where it was (the collector asks before downloading a file).
		if fi, err := r.Root.Stat(name); err == nil && fi.Mode().IsRegular() {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}
	if err := r.write(name, http.MaxBytesReader(w, req.Body, MaxFile)); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Logf("receiver: a file not written: %v", err)
		http.Error(w, "not written", http.StatusInternalServerError)
		return
	}
	if n := r.files.Add(1); n%1000 == 0 || name == "manifest.json" {
		r.Logf("receiver: %d files", n)
	}
	w.WriteHeader(http.StatusNoContent)
}

// write puts body at name, whole or not at all: a temporary file renamed.
func (r *Receiver) write(name string, body io.Reader) error {
	if dir := path.Dir(name); dir != "." {
		if err := r.Root.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	tmp := name + ".part"
	f, err := r.Root.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		_ = r.Root.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = r.Root.Remove(tmp)
		return err
	}
	return r.Root.Rename(tmp, name)
}
