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

package oauth

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// What one address may ask of the provider. A login request is a page a
// person opens, a few a minute at most; its page polls every 2 s; a site's
// server exchanges a code per login. Above that the provider answers 429,
// which keeps a script from filling Redis with requests or guessing codes.
type limitKind int

const (
	limitAuthorize limitKind = iota
	limitState
	limitToken
	limitKinds
)

var limitRates = [limitKinds]struct {
	every time.Duration
	burst int
}{
	limitAuthorize: {every: 6 * time.Second, burst: 10}, // 10 a minute
	limitState:     {every: 250 * time.Millisecond, burst: 20},
	limitToken:     {every: time.Second, burst: 30},
}

// limitsPurgeAt is the number of addresses after which the idle ones are
// forgotten.
const limitsPurgeAt = 4096

type limits struct {
	mu   sync.Mutex
	byIP map[string]*[limitKinds]*rate.Limiter
}

func newLimits() *limits {
	return &limits{byIP: map[string]*[limitKinds]*rate.Limiter{}}
}

// subject is the address a limit counts: an IPv4 address, or the /64 of an
// IPv6 one, which one host usually has whole.
func subject(ip string) string {
	a := net.ParseIP(ip)
	if a == nil {
		return ip
	}
	if a4 := a.To4(); a4 != nil {
		return a4.String()
	}
	return a.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// allow tells whether ip may make one more request of kind now.
func (l *limits) allow(kind limitKind, ip string, now time.Time) bool {
	key := subject(ip)
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.byIP[key]
	if st == nil {
		if len(l.byIP) >= limitsPurgeAt {
			l.purgeLocked(now)
		}
		st = new([limitKinds]*rate.Limiter)
		for k := range st {
			st[k] = rate.NewLimiter(rate.Every(limitRates[k].every), limitRates[k].burst)
		}
		l.byIP[key] = st
	}
	return st[kind].AllowN(now, 1)
}

// purgeLocked forgets the addresses whose limiters are full again: they
// would start the same.
func (l *limits) purgeLocked(now time.Time) {
	for key, st := range l.byIP {
		idle := true
		for k, lim := range st {
			if lim.TokensAt(now) < float64(limitRates[k].burst) {
				idle = false
				break
			}
		}
		if idle {
			delete(l.byIP, key)
		}
	}
}
