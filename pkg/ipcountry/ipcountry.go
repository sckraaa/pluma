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

// Package ipcountry tells the country of a client's address, by which a
// client is routed to the edges of pluma_edge and the CDN deliveries of its
// country (task 109, ADR 0072). The GeoIP database is only in authsession
// (authsession.getIpCountry); a Locator caches its answers, so that
// help.getAppConfig and upload.getFile do not call authsession every time.
package ipcountry

import (
	"container/list"
	"context"
	"net/netip"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// The bounds of the cache of a Locator. A client's address seldom changes
// within the hour, and 10 000 addresses are far more than the clients of a
// server of about 100 users use in an hour; an entry takes about 100 bytes.
// The database itself changes twice a week (docs/ops/deployment.md,
// "GeoIP"): an hour-old answer is as good as a new one.
const (
	CacheSize = 10000
	CacheTTL  = time.Hour
)

// metricErrors counts the lookups authsession did not answer. Such a
// lookup tells no country, and the client is routed as one of an unknown
// country (the `*` routes) until authsession answers again.
var metricErrors = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "pluma",
	Subsystem: "ipcountry",
	Name:      "errors_total",
	Help:      "Lookups of the country of a client's address that authsession.getIpCountry failed: the client was routed as one of an unknown country.",
	Labels:    []string{},
})

// Client is the part of the authsession client a Locator uses.
type Client interface {
	AuthsessionGetIpCountry(ctx context.Context, in *authsession.TLAuthsessionGetIpCountry) (*mtproto.String, error)
}

// Locator tells the country of an address as authsession.getIpCountry
// does, from a cache of the last CacheSize addresses looked up, each kept
// for CacheTTL. An answer that the country is not known is cached as well;
// a failed lookup is not. It is safe for concurrent use.
type Locator struct {
	client Client
	size   int
	ttl    time.Duration
	now    func() time.Time

	mu    sync.Mutex
	order *list.List // of *entry, the most recently used first
	byIP  map[netip.Addr]*list.Element
}

type entry struct {
	ip      netip.Addr
	country string
	at      time.Time
}

// New returns a Locator over client with the cache of CacheSize addresses
// for CacheTTL.
func New(client Client) *Locator {
	return newLocator(client, CacheSize, CacheTTL, time.Now)
}

func newLocator(client Client, size int, ttl time.Duration, now func() time.Time) *Locator {
	return &Locator{
		client: client,
		size:   size,
		ttl:    ttl,
		now:    now,
		order:  list.New(),
		byIP:   map[netip.Addr]*list.Element{},
	}
}

// Country is the country of the address ip (as MD.ClientAddr gives it, an
// IPv4 or IPv6 address without a port): an ISO 3166-1 alpha-2 code in upper
// case, or "" when it is not known, also when ip is not an address or the
// lookup failed. A failure is counted in pluma_ipcountry_errors_total and
// logged without the address.
func (l *Locator) Country(ctx context.Context, ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Zone() != "" {
		// authsession knows no country of such an address (net.ParseIP
		// takes no zone): no call is spent on it.
		return ""
	}
	// An IPv4 address written as IPv4-mapped IPv6 is the same address, one
	// entry and one lookup.
	addr = addr.Unmap()

	if country, ok := l.cached(addr); ok {
		return country
	}
	r, err := l.client.AuthsessionGetIpCountry(ctx, &authsession.TLAuthsessionGetIpCountry{Ip: addr.String()})
	if err != nil {
		metricErrors.Inc()
		logx.WithContext(ctx).Errorf("ipcountry: authsession.getIpCountry: %v", err)
		return ""
	}
	country := r.GetV()
	l.put(addr, country)
	return country
}

// cached is the country of addr in the cache, if it is there and not older
// than ttl.
func (l *Locator) cached(addr netip.Addr) (string, bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.byIP[addr]
	if !ok {
		return "", false
	}
	e := el.Value.(*entry)
	// A clock that went back makes the entry as unusable as an old one.
	if age := now.Sub(e.at); age >= l.ttl || age < 0 {
		l.drop(el)
		return "", false
	}
	l.order.MoveToFront(el)
	return e.country, true
}

// put caches the country of addr, dropping the least recently used entries
// past size.
func (l *Locator) put(addr netip.Addr, country string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.byIP[addr]; ok {
		// Another lookup of the same address finished first.
		l.drop(el)
	}
	l.byIP[addr] = l.order.PushFront(&entry{ip: addr, country: country, at: now})
	for l.order.Len() > l.size {
		l.drop(l.order.Back())
	}
}

// drop removes el; l.mu is held.
func (l *Locator) drop(el *list.Element) {
	l.order.Remove(el)
	delete(l.byIP, el.Value.(*entry).ip)
}
