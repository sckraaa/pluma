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

package cdn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

const testDc = 201

// testConfig is the config of an enabled CDN for the tests: one Gcore
// delivery with throwaway secrets.
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Deliveries: []DeliveryConfig{{
			Id:      testDc,
			Name:    "gcore",
			BaseURL: "https://cdn.example.com",
			Key:     hex.EncodeToString(randomBytes(t, 16)),
		}},
		Origin: OriginConfig{AuthValue: hex.EncodeToString(randomBytes(t, 32))},
	}
}

// testSettings are the parsed testConfig.
func testSettings(t *testing.T) *Settings {
	t.Helper()
	s, err := testConfig(t).Parse()
	if err != nil || s == nil {
		t.Fatalf("test settings: %v", err)
	}
	return s
}

// clock is a time the tests move by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// randomBytes returns n random bytes.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeFiles is a Sizer over files in memory, keyed as cdn_files keys them.
type fakeFiles struct {
	mu      sync.Mutex
	files   map[FileKey][]byte
	sizeErr error
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{files: map[FileKey][]byte{}}
}

func (f *fakeFiles) add(key FileKey, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[key] = b
}

func (f *fakeFiles) FileSize(_ context.Context, loc *mtproto.InputFileLocation) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sizeErr != nil {
		return 0, f.sizeErr
	}
	key, _ := Key(loc)
	b, ok := f.files[key]
	if !ok {
		return 0, ErrNoFile
	}
	return int64(len(b)), nil
}
