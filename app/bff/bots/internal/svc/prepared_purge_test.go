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

package svc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePurger struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *fakePurger) PurgeExpiredPrepared(ctx context.Context, now int32) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if now < int32(time.Now().Unix())-5 {
		return 0, errors.New("stale now")
	}
	if f.fail {
		return 0, errors.New("down")
	}
	return 3, nil
}

func (f *fakePurger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// The purge runs at the start, again every period, a failed round does not
// stop the next, and it ends with its context.
func TestPurgePreparedEvery(t *testing.T) {
	f := &fakePurger{fail: true}
	ctx, cancel := context.WithCancel(context.Background())
	purgePreparedEvery(ctx, f, 20*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for f.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.count() < 3 {
		t.Fatalf("rounds: %d", f.count())
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	after := f.count()
	time.Sleep(100 * time.Millisecond)
	if f.count() != after {
		t.Fatalf("rounds after the end: %d then %d", after, f.count())
	}
}
