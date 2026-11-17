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

package ipcountry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/zeromicro/go-zero/core/logx/logtest"
	"github.com/zeromicro/go-zero/core/prometheus"
)

// go-zero records metrics only once Prometheus is enabled.
func init() { prometheus.Enable() }

// errorsTotal is the value of pluma_ipcountry_errors_total.
func errorsTotal(t *testing.T) float64 {
	t.Helper()
	families, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "pluma_ipcountry_errors_total" {
			for _, m := range f.GetMetric() {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// authsessionStub answers authsession.getIpCountry from countries, and
// fails while fail is set. It counts the calls of each address.
type authsessionStub struct {
	mu        sync.Mutex
	countries map[string]string
	fail      error
	calls     map[string]int
}

func newStub(countries map[string]string) *authsessionStub {
	return &authsessionStub{countries: countries, calls: map[string]int{}}
}

func (s *authsessionStub) AuthsessionGetIpCountry(_ context.Context, in *authsession.TLAuthsessionGetIpCountry) (*mtproto.String, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[in.GetIp()]++
	if s.fail != nil {
		return nil, s.fail
	}
	return mtproto.MakeTLString(&mtproto.String{V: s.countries[in.GetIp()]}).To_String(), nil
}

func (s *authsessionStub) callsOf(ip string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[ip]
}

func (s *authsessionStub) setFail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

// clock is a time a test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// New keeps 10 000 addresses for an hour, as the brief of task 109 sets.
func TestNewBounds(t *testing.T) {
	l := New(newStub(nil))
	if l.size != 10000 || l.ttl != time.Hour {
		t.Fatalf("cache of %d addresses for %v, want 10000 for 1h", l.size, l.ttl)
	}
}

// An address is looked up once while its answer is cached, an unknown
// country included; an IPv4 address written as IPv4-mapped IPv6 is the
// same address.
func TestCountryCached(t *testing.T) {
	stub := newStub(map[string]string{"95.108.198.1": "RU", "2a02:6b8::1": "RU"})
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := newLocator(stub, CacheSize, CacheTTL, c.now)
	ctx := context.Background()

	for _, ip := range []string{"95.108.198.1", "::ffff:95.108.198.1", "95.108.198.1"} {
		if got := l.Country(ctx, ip); got != "RU" {
			t.Fatalf("%q: %q, want RU", ip, got)
		}
	}
	if n := stub.callsOf("95.108.198.1"); n != 1 {
		t.Errorf("IPv4 address looked up %d times, want once", n)
	}
	for range 2 {
		if got := l.Country(ctx, "2a02:06b8:0::1"); got != "RU" {
			t.Fatalf("IPv6: %q, want RU", got)
		}
	}
	if n := stub.callsOf("2a02:6b8::1"); n != 1 {
		t.Errorf("IPv6 address looked up %d times as 2a02:6b8::1, want once", n)
	}
	for range 2 {
		if got := l.Country(ctx, "193.99.144.80"); got != "" {
			t.Fatalf("unknown country: %q, want none", got)
		}
	}
	if n := stub.callsOf("193.99.144.80"); n != 1 {
		t.Errorf("address of no country looked up %d times, want once", n)
	}

	// An hour later the answer is old, and the address is looked up again;
	// so it is after the clock went back.
	c.t = c.t.Add(time.Hour)
	l.Country(ctx, "95.108.198.1")
	if n := stub.callsOf("95.108.198.1"); n != 2 {
		t.Errorf("after an hour: %d lookups, want 2", n)
	}
	c.t = c.t.Add(-time.Minute)
	l.Country(ctx, "95.108.198.1")
	if n := stub.callsOf("95.108.198.1"); n != 3 {
		t.Errorf("after the clock went back: %d lookups, want 3", n)
	}
}

// What is no address costs no lookup.
func TestCountryNotAnAddress(t *testing.T) {
	stub := newStub(nil)
	l := New(stub)
	for _, ip := range []string{"", "not an address", "95.108.198.1:443", "[2a02:6b8::1]", "fe80::1%eth0"} {
		if got := l.Country(context.Background(), ip); got != "" {
			t.Errorf("%q: %q, want none", ip, got)
		}
	}
	if len(stub.calls) != 0 {
		t.Errorf("looked up %v", stub.calls)
	}
}

// A failed lookup tells no country and is not cached: the next asks
// authsession again. It is counted, and the log does not name the address.
func TestCountryErrorNotCached(t *testing.T) {
	stub := newStub(map[string]string{"95.108.198.1": "RU"})
	l := New(stub)
	ctx := context.Background()
	logs := logtest.NewCollector(t)
	before := errorsTotal(t)

	stub.setFail(errors.New("rpc error: code = Unavailable desc = connection refused"))
	if got := l.Country(ctx, "95.108.198.1"); got != "" {
		t.Fatalf("failed lookup: %q, want none", got)
	}
	if got := errorsTotal(t) - before; got != 1 {
		t.Errorf("errors counted %v, want 1", got)
	}
	if out := logs.String(); !strings.Contains(out, "Unavailable") || strings.Contains(out, "95.108") {
		t.Errorf("logged %q, want the error without the address", out)
	}

	stub.setFail(nil)
	if got := l.Country(ctx, "95.108.198.1"); got != "RU" {
		t.Fatalf("after authsession came back: %q, want RU", got)
	}
	if n := stub.callsOf("95.108.198.1"); n != 2 {
		t.Errorf("%d lookups, want 2: the failure must not be cached", n)
	}
}

// Past its size the cache drops the address used least recently.
func TestCountryEvictsLeastRecentlyUsed(t *testing.T) {
	stub := newStub(map[string]string{"1.1.1.1": "AU", "8.8.8.8": "US", "95.108.198.1": "RU"})
	l := newLocator(stub, 2, CacheTTL, time.Now)
	ctx := context.Background()

	l.Country(ctx, "1.1.1.1")
	l.Country(ctx, "8.8.8.8")
	l.Country(ctx, "1.1.1.1")      // used again: 8.8.8.8 is now the least recent
	l.Country(ctx, "95.108.198.1") // one more than the cache holds
	if n := len(l.byIP); n != 2 || l.order.Len() != 2 {
		t.Fatalf("%d addresses in the map, %d in the list, want 2", n, l.order.Len())
	}

	l.Country(ctx, "1.1.1.1")
	l.Country(ctx, "95.108.198.1")
	if n, m := stub.callsOf("1.1.1.1"), stub.callsOf("95.108.198.1"); n != 1 || m != 1 {
		t.Errorf("cached addresses looked up again: %d and %d times", n, m)
	}
	if got := l.Country(ctx, "8.8.8.8"); got != "US" {
		t.Fatalf("evicted address: %q, want US", got)
	}
	if n := stub.callsOf("8.8.8.8"); n != 2 {
		t.Errorf("evicted address looked up %d times, want 2", n)
	}
}

// Lookups from many goroutines keep the cache whole.
func TestCountryConcurrent(t *testing.T) {
	stub := newStub(map[string]string{"1.1.1.1": "AU", "8.8.8.8": "US", "95.108.198.1": "RU"})
	l := newLocator(stub, 2, CacheTTL, time.Now)
	want := map[string]string{"1.1.1.1": "AU", "8.8.8.8": "US", "95.108.198.1": "RU"}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ips := []string{"1.1.1.1", "8.8.8.8", "95.108.198.1"}
			for j := range 300 {
				ip := ips[(i+j)%len(ips)]
				if got := l.Country(context.Background(), ip); got != want[ip] {
					t.Errorf("%s: %q, want %q", ip, got, want[ip])
					return
				}
			}
		}()
	}
	wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.byIP) != l.order.Len() || l.order.Len() > 2 {
		t.Fatalf("%d addresses in the map, %d in the list, want the same and at most 2", len(l.byIP), l.order.Len())
	}
}
