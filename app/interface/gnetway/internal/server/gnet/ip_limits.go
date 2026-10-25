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

package gnet

import (
	"net"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/time/rate"
)

// ipLimits bounds what one client address may do to the gateway (task 008):
// the connections it holds, and the key exchanges it starts (req_DH_params
// costs the server an RSA decryption and a DH exponentiation). An IPv6
// address counts by its /64, which one client usually holds whole. The
// defaults leave room for a group behind one NAT.
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

const (
	defaultMaxConnsPerIP           = 512
	defaultKeyExchangesPerIPMinute = 300
	// ipStatesPurgeAt is how many addresses the limits remember before they
	// forget the idle ones (no connection, a full key exchange budget).
	ipStatesPurgeAt = 10000
)

// newIPLimits returns the limits: maxConns connections per address and
// keyExchangesPerMinute req_DH_params per address and minute, as a burst
// of that many; 0 takes the default.
func newIPLimits(maxConns, keyExchangesPerMinute int) *ipLimits {
	if maxConns <= 0 {
		maxConns = defaultMaxConnsPerIP
	}
	if keyExchangesPerMinute <= 0 {
		keyExchangesPerMinute = defaultKeyExchangesPerIPMinute
	}
	return &ipLimits{
		maxConns: maxConns,
		dhRate:   rate.Every(time.Minute / time.Duration(keyExchangesPerMinute)),
		dhBurst:  keyExchangesPerMinute,
		byIP:     make(map[string]*ipState),
	}
}

// ipSubject returns what the limits count ip by: the address, or for IPv6
// its /64.
func ipSubject(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if parsed.To4() != nil {
		return parsed.String()
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func (l *ipLimits) stateLocked(subject string) *ipState {
	st := l.byIP[subject]
	if st == nil {
		if len(l.byIP) >= ipStatesPurgeAt {
			l.purgeLocked(time.Now())
		}
		st = &ipState{dh: rate.NewLimiter(l.dhRate, l.dhBurst)}
		l.byIP[subject] = st
	}
	return st
}

func (l *ipLimits) purgeLocked(now time.Time) {
	for subject, st := range l.byIP {
		if st.conns == 0 && st.dh.TokensAt(now) >= float64(l.dhBurst) {
			delete(l.byIP, subject)
		}
	}
}

// openConn counts a new connection of subject; false when subject holds
// the most connections already (the connection is to be closed).
func (l *ipLimits) openConn(subject string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.stateLocked(subject)
	if st.conns >= l.maxConns {
		return false
	}
	st.conns++
	return true
}

// closeConn uncounts a connection openConn counted.
func (l *ipLimits) closeConn(subject string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if st := l.byIP[subject]; st != nil && st.conns > 0 {
		st.conns--
	}
}

// allowKeyExchange takes one key exchange of subject at now; false when it
// started too many.
func (l *ipLimits) allowKeyExchange(subject string, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stateLocked(subject).dh.AllowN(now, 1)
}

// countConn counts connection c against its address once the address is
// known; false (and a log line) when the address holds too many.
func (s *Server) countConn(c gnet.Conn, ctx *connContext) bool {
	if ctx.limitSubject != "" {
		return true
	}
	subject := ipSubject(ctx.clientIp)
	if !s.limits.openConn(subject) {
		metricLimitRefused.Inc("conn")
		logx.Errorf("conn(%d) refused: %s holds too many connections", c.ConnId(), subject)
		return false
	}
	ctx.limitSubject = subject
	return true
}
