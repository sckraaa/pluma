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

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
)

const egressFile = "egress.json"

// Egress counts the bytes a node sends to clients in the current month
// (UTC), the figure its traffic budget is held to (CDN_MONTHLY_EGRESS_LIMIT_GB
// at the home). The count is kept in egress.json, written by Flush, so
// that a restart does not reset it. It is safe for concurrent use.
type Egress struct {
	path string
	now  func() time.Time

	mu    sync.Mutex
	month string
	bytes int64
	dirty bool
}

type egressDoc struct {
	Month string `json:"month"`
	Bytes int64  `json:"bytes"`
}

// OpenEgress reads the count kept in dir; now is time.Now when nil.
func OpenEgress(dir string, now func() time.Time) (*Egress, error) {
	if now == nil {
		now = time.Now
	}
	e := &Egress{path: filepath.Join(dir, egressFile), now: now}
	b, err := os.ReadFile(e.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("store: %s: %w", egressFile, err)
	default:
		var doc egressDoc
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("store: %s: %w", egressFile, err)
		}
		if doc.Bytes < 0 {
			return nil, fmt.Errorf("store: %s: negative count", egressFile)
		}
		e.month, e.bytes = doc.Month, doc.Bytes
	}
	e.mu.Lock()
	e.rollLocked()
	metrics.EgressMonthBytes.Set(float64(e.bytes))
	e.mu.Unlock()
	return e, nil
}

// rollLocked starts a new count when the month changed.
func (e *Egress) rollLocked() {
	if m := e.now().UTC().Format("2006-01"); m != e.month {
		e.month, e.bytes, e.dirty = m, 0, true
	}
}

// Add counts n bytes sent.
func (e *Egress) Add(n int) {
	if n <= 0 {
		return
	}
	e.mu.Lock()
	e.rollLocked()
	e.bytes += int64(n)
	e.dirty = true
	v := e.bytes
	e.mu.Unlock()
	metrics.EgressMonthBytes.Set(float64(v))
}

// Month returns the current month (YYYY-MM) and the bytes sent in it.
func (e *Egress) Month() (string, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rollLocked()
	return e.month, e.bytes
}

// Flush writes the count if it changed. It runs once a minute and at
// shutdown; a crash loses at most the last minute.
func (e *Egress) Flush() error {
	e.mu.Lock()
	e.rollLocked()
	if !e.dirty {
		e.mu.Unlock()
		return nil
	}
	doc := egressDoc{Month: e.month, Bytes: e.bytes}
	e.dirty = false
	e.mu.Unlock()
	if err := writeFileAtomic(e.path, doc); err != nil {
		e.mu.Lock()
		e.dirty = true
		e.mu.Unlock()
		return fmt.Errorf("store: write %s: %w", egressFile, err)
	}
	return nil
}
