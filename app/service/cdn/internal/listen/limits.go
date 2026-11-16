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

package listen

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipLimits bounds what one address may hold: open connections, and key
// exchanges as a token bucket. It repeats the gateway's limits
// (app/interface/gnetway/internal/server/gnet/ip_limits.go) with the
// node's numbers; an IPv6 address counts by its /64.
type ipLimits struct {
	mu       sync.Mutex
	maxConns int
	dhRate   rate.Limit
	dhBurst  int
	byIP     map[string]*ipState
}

type ipState struct {
	conns int
	dh    *rate.Limiter
}

// ipStatesPurgeAt is how many addresses the limits remember before they
// forget the idle ones (no connection, a full key exchange budget).
const ipStatesPurgeAt = 10000

func newIPLimits(maxConns, keyExchangesPerMinute int) *ipLimits {
	return &ipLimits{
		maxConns: maxConns,
		dhRate:   rate.Every(time.Minute / time.Duration(keyExchangesPerMinute)),
		dhBurst:  keyExchangesPerMinute,
		byIP:     make(map[string]*ipState),
	}
}

// ipSubject is what an address counts as.
func ipSubject(addr net.Addr) string {
	var ip net.IP
	if a, ok := addr.(*net.TCPAddr); ok {
		ip = a.IP
	} else if host, _, err := net.SplitHostPort(addr.String()); err == nil {
		ip = net.ParseIP(host)
	}
	if ip == nil {
		return addr.String()
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func (l *ipLimits) stateLocked(subject string) *ipState {
	st := l.byIP[subject]
	if st == nil {
		if len(l.byIP) >= ipStatesPurgeAt {
			now := time.Now()
			for s, v := range l.byIP {
				if v.conns == 0 && v.dh.TokensAt(now) >= float64(l.dhBurst) {
					delete(l.byIP, s)
				}
			}
		}
		st = &ipState{dh: rate.NewLimiter(l.dhRate, l.dhBurst)}
		l.byIP[subject] = st
	}
	return st
}

func (l *ipLimits) openConn(subject string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.stateLocked(subject)
	if st.conns >= l.maxConns {
		return false
	}
	st.conns++
	return true
}

func (l *ipLimits) closeConn(subject string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st := l.byIP[subject]; st != nil && st.conns > 0 {
		st.conns--
	}
}

func (l *ipLimits) allowKeyExchange(subject string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stateLocked(subject).dh.AllowN(now, 1)
}
