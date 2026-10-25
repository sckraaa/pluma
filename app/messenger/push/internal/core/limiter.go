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

package core

import (
	"sync"
	"time"
)

// limiter allows one event per device and dialog every period. It lives in
// memory: after a restart a dialog may get one more.
type limiter struct {
	mu     sync.Mutex
	period time.Duration
	last   map[limiterKey]time.Time
	now    func() time.Time
}

type limiterKey struct {
	device int64
	peer   int64
	chat   bool
}

func newLimiter(period time.Duration) *limiter {
	return &limiter{period: period, last: map[limiterKey]time.Time{}, now: time.Now}
}

func (l *limiter) allow(device, peer int64, chat bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	k := limiterKey{device, peer, chat}
	if t, ok := l.last[k]; ok && now.Sub(t) < l.period {
		return false
	}
	l.last[k] = now
	if len(l.last) > 4096 {
		for k, t := range l.last {
			if now.Sub(t) >= l.period {
				delete(l.last, k)
			}
		}
	}
	return true
}
