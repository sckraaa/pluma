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
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeGate keeps out the deliveries it has a result for; err fails every
// call.
type fakeGate struct {
	mu  sync.Mutex
	out map[int32]string
	err error
}

func (g *fakeGate) Out(_ context.Context, d *Delivery, _ time.Time) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err != nil {
		return ResultError, g.err
	}
	return g.out[d.ID], nil
}

func (g *fakeGate) set(dc int32, out string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.out[dc] = out
}

// twoDeliveries are settings with deliveries 201 and 202, in that order of
// preference.
func twoDeliveries(t *testing.T) *Settings {
	t.Helper()
	c := testConfig(t)
	second := c.Deliveries[0]
	second.Id, second.Name, second.BaseURL = 202, "second", "https://cdn2.example.com"
	second.OriginAuth = hex.EncodeToString(randomBytes(t, 32))
	c.Deliveries = append(c.Deliveries, second)
	c.Routes = "*:201,202"
	set, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func pick(t *testing.T, r *Router, key int64, now time.Time) (int32, string) {
	t.Helper()
	d, result, err := r.Pick(context.Background(), key, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		return 0, result
	}
	return d.ID, result
}

// TestRouterPick: the first delivery of the route that is not out; with
// none, the reason of the preferred one.
func TestRouterPick(t *testing.T) {
	gate := &fakeGate{out: map[int32]string{}}
	r, err := NewRouter(twoDeliveries(t), gate)
	if err != nil {
		t.Fatal(err)
	}
	now := newClock().now()
	if dc, _ := pick(t, r, 1, now); dc != 201 {
		t.Fatalf("picked %d, want 201", dc)
	}
	gate.set(201, ResultUnhealthy)
	if dc, _ := pick(t, r, 2, now); dc != 202 {
		t.Fatalf("201 out: picked %d, want 202", dc)
	}
	gate.set(202, ResultBudget)
	if dc, result := pick(t, r, 3, now); dc != 0 || result != ResultUnhealthy {
		t.Fatalf("both out: %d, %q", dc, result)
	}
	gate.err = errors.New("mysql is down")
	if d, result, err := r.Pick(context.Background(), 4, "", now); d != nil || result != ResultError || err == nil {
		t.Fatalf("gate failed: %v, %q, %v", d, result, err)
	}
}

// TestRouterHold: a key keeps its delivery for pickHold while it takes
// redirects, even when a preferred one comes back; a delivery that goes
// out is left at once.
func TestRouterHold(t *testing.T) {
	gate := &fakeGate{out: map[int32]string{201: ResultUnhealthy}}
	r, err := NewRouter(twoDeliveries(t), gate)
	if err != nil {
		t.Fatal(err)
	}
	c := newClock()
	if dc, _ := pick(t, r, 1, c.now()); dc != 202 {
		t.Fatalf("picked %d, want 202", dc)
	}
	gate.set(201, "")
	c.add(pickHold - time.Second)
	if dc, _ := pick(t, r, 1, c.now()); dc != 202 {
		t.Fatalf("within the hold: %d, want 202", dc)
	}
	if dc, _ := pick(t, r, 2, c.now()); dc != 201 {
		t.Fatalf("another key: %d, want 201", dc)
	}
	c.add(time.Second)
	if dc, _ := pick(t, r, 1, c.now()); dc != 201 {
		t.Fatalf("after the hold: %d, want 201", dc)
	}

	// The held delivery goes out: the key moves on at once.
	gate.set(201, ResultUnhealthy)
	if dc, _ := pick(t, r, 1, c.now()); dc != 202 {
		t.Fatalf("held one out: %d, want 202", dc)
	}
}

// TestRouterFallback: a request without cdn_supported within
// fallbackWindow of a redirect makes the key avoid that delivery for
// AvoidTTL, and moves the key to the next one; each redirect counts once.
func TestRouterFallback(t *testing.T) {
	set := twoDeliveries(t)
	gate := &fakeGate{out: map[int32]string{}}
	r, err := NewRouter(set, gate)
	if err != nil {
		t.Fatal(err)
	}
	c := newClock()
	file := FileKey{Kind: KindDocument, Id: 9}
	dc, _ := pick(t, r, 1, c.now())
	r.Redirected(1, file, dc, c.now())

	if _, ok := r.FellBack(2, file, c.now()); ok {
		t.Fatal("another key fell back")
	}
	if _, ok := r.FellBack(1, FileKey{Kind: KindDocument, Id: 10}, c.now()); ok {
		t.Fatal("a file never redirected fell back")
	}
	c.add(fallbackWindow - time.Second)
	got, ok := r.FellBack(1, file, c.now())
	if !ok || got != 201 {
		t.Fatalf("fallback: %d, %v", got, ok)
	}
	if _, ok = r.FellBack(1, file, c.now()); ok {
		t.Fatal("one redirect fell back twice")
	}
	if dc, _ := pick(t, r, 1, c.now()); dc != 202 {
		t.Fatalf("after the fallback: %d, want 202", dc)
	}
	r.Redirected(1, file, 202, c.now())
	if got, ok = r.FellBack(1, file, c.now()); !ok || got != 202 {
		t.Fatalf("fallback from 202: %d, %v", got, ok)
	}
	if dc, result := pick(t, r, 1, c.now()); dc != 0 || result != ResultAvoided {
		t.Fatalf("both avoided: %d, %q", dc, result)
	}
	if dc, _ := pick(t, r, 2, c.now()); dc != 201 {
		t.Fatalf("another key: %d, want 201", dc)
	}
	c.add(set.AvoidTTL)
	if dc, _ := pick(t, r, 1, c.now()); dc != 201 {
		t.Fatalf("after AvoidTTL: %d, want 201", dc)
	}

	// Past the window a request without the flag is no fallback.
	r.Redirected(1, file, 201, c.now())
	c.add(fallbackWindow)
	if _, ok = r.FellBack(1, file, c.now()); ok {
		t.Fatal("a fallback after the window")
	}
}

// countryDeliveries are twoDeliveries routed by country: Russia and
// Belarus to 202, then 201; every other client to 201.
func countryDeliveries(t *testing.T) *Settings {
	t.Helper()
	set := twoDeliveries(t)
	routes, err := ParseRoutes("RU,BY:202,201;*:201")
	if err != nil {
		t.Fatal(err)
	}
	set.Routes = routes
	return set
}

func pickIn(t *testing.T, r *Router, key int64, country string, now time.Time) (int32, string) {
	t.Helper()
	d, result, err := r.Pick(context.Background(), key, country, now)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		return 0, result
	}
	return d.ID, result
}

// TestRouterCountry: a client is routed by the record of its country, an
// unknown country or one no record names by the record "*".
func TestRouterCountry(t *testing.T) {
	gate := &fakeGate{out: map[int32]string{}}
	r, err := NewRouter(countryDeliveries(t), gate)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ByCountry() || r.First().ID != 201 || r.FirstOf("RU").ID != 202 || r.FirstOf("DE").ID != 201 || r.FirstOf("").ID != 201 {
		t.Fatalf("ByCountry %v, first %d", r.ByCountry(), r.First().ID)
	}
	now := newClock().now()
	for i, c := range []struct {
		country string
		want    int32
	}{{"RU", 202}, {"BY", 202}, {"DE", 201}, {"", 201}, {"ru", 201}} {
		if dc, _ := pickIn(t, r, int64(i+1), c.country, now); dc != c.want {
			t.Errorf("%q: picked %d, want %d", c.country, dc, c.want)
		}
	}
	// 202 out: Russia takes the next of its record, the others never 202.
	gate.set(202, ResultUnhealthy)
	if dc, _ := pickIn(t, r, 10, "RU", now); dc != 201 {
		t.Errorf("RU with 202 out: %d, want 201", dc)
	}
	gate.set(201, ResultBudget)
	if dc, result := pickIn(t, r, 11, "RU", now); dc != 0 || result != ResultUnhealthy {
		t.Errorf("RU with both out: %d, %q, want the reason of 202", dc, result)
	}
	if dc, result := pickIn(t, r, 12, "DE", now); dc != 0 || result != ResultBudget {
		t.Errorf("DE with 201 out: %d, %q", dc, result)
	}
	if ts := r.Targets(); !slices.Equal(ts, []int32{TargetHomeOrigin}) {
		t.Errorf("targets %v", ts)
	}
}

// TestRouterCountryHold: the hold of a key belongs to the record it was
// picked by: a client whose country changes (a VPN on or off) is routed by
// its new record at once; one whose new country is in the same record
// keeps its delivery.
func TestRouterCountryHold(t *testing.T) {
	gate := &fakeGate{out: map[int32]string{202: ResultUnhealthy}}
	r, err := NewRouter(countryDeliveries(t), gate)
	if err != nil {
		t.Fatal(err)
	}
	c := newClock()
	if dc, _ := pickIn(t, r, 1, "RU", c.now()); dc != 201 {
		t.Fatalf("RU with 202 out: %d, want 201", dc)
	}
	gate.set(202, "")
	c.add(time.Minute)
	if dc, _ := pickIn(t, r, 1, "BY", c.now()); dc != 201 {
		t.Fatalf("within the hold, a country of the same record: %d, want 201", dc)
	}
	if dc, _ := pickIn(t, r, 2, "RU", c.now()); dc != 202 {
		t.Fatalf("another key: %d, want 202", dc)
	}

	// Key 2 holds 202 by the record of Russia; from Germany it is routed by
	// "*" at once, and back in Russia by its record again, which the hold
	// of "*" does not keep from 202.
	if dc, _ := pickIn(t, r, 2, "DE", c.now()); dc != 201 {
		t.Fatalf("the country changed: %d, want 201", dc)
	}
	if dc, _ := pickIn(t, r, 2, "RU", c.now()); dc != 202 {
		t.Fatalf("back in Russia: %d, want 202", dc)
	}
	// An unknown country is the record "*".
	if dc, _ := pickIn(t, r, 2, "", c.now()); dc != 201 {
		t.Fatalf("the country not known: %d, want 201", dc)
	}
}

// TestNewRouterRoutes: every record names deliveries that are on, and
// exactly one is "*".
func TestNewRouterRoutes(t *testing.T) {
	gate := &fakeGate{out: map[int32]string{}}
	for name, routes := range map[string][]Route{
		"no record *":       {{Countries: []string{"RU"}, Deliveries: []int32{202}}},
		"two records *":     {{Deliveries: []int32{201}}, {Deliveries: []int32{202}}},
		"a country twice":   {{Countries: []string{"RU"}, Deliveries: []int32{202}}, {Countries: []string{"RU"}, Deliveries: []int32{201}}, {Deliveries: []int32{201}}},
		"no deliveries":     {{Countries: []string{"RU"}}, {Deliveries: []int32{201}}},
		"a delivery not on": {{Countries: []string{"RU"}, Deliveries: []int32{203}}, {Deliveries: []int32{201}}},
		"no record at all":  nil,
	} {
		set := twoDeliveries(t)
		set.Routes = routes
		if _, err := NewRouter(set, gate); err == nil {
			t.Errorf("%s: a router", name)
		}
	}
	r, err := NewRouter(twoDeliveries(t), gate)
	if err != nil || r.ByCountry() {
		t.Fatalf("one record *: %v, ByCountry %v", err, r != nil && r.ByCountry())
	}
}

// fakeLocator tells the countries of addresses from a map and counts the
// lookups.
type fakeLocator struct {
	mu      sync.Mutex
	country map[string]string
	lookups int
}

func (l *fakeLocator) Country(_ context.Context, ip string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lookups++
	return l.country[ip]
}

// TestRedirectByCountry: a client from Russia is redirected to 202, any
// other to 201; the country is looked up only for a client that may be
// redirected, and an address of no known country takes the record "*".
func TestRedirectByCountry(t *testing.T) {
	set := countryDeliveries(t)
	countries := &fakeLocator{country: map[string]string{"5.8.16.10": "RU", "198.51.100.4": "DE"}}
	e := &homeEnv{
		t: t, ctx: context.Background(), set: set, store: NewMemStore(), files: newFakeFiles(),
		caps: &fakeCaps{versions: map[int64]int{testKey: 1}}, clock: newClock(),
	}
	var err error
	if e.home, err = NewHome(e.store, e.set, e.files, e.caps, countries); err != nil {
		t.Fatal(err)
	}
	e.home.now = e.clock.now
	e.healthyDc(201, true)
	e.healthyDc(202, true)
	e.files.add(FileKey{Kind: KindDocument, Id: 50}, randomBytes(t, 2<<20))
	e.home.Redirect(e.ctx, testKey, "5.8.16.10", docLoc(50), 0)
	e.prepareAll()

	for addr, want := range map[string]int32{"5.8.16.10": 202, "198.51.100.4": 201, testAddr: 201, "": 201} {
		res, result := e.home.Redirect(e.ctx, testKey, addr, docLoc(50), 0)
		if result != ResultRedirect || res.GetDcId() != want {
			t.Errorf("from %q: %q to %d, want %d", addr, result, res.GetDcId(), want)
		}
	}

	// A client without pluma_cdn is never looked up.
	before := countries.lookups
	if res, result := e.home.Redirect(e.ctx, testKey+1, "5.8.16.10", docLoc(50), 0); res != nil || result != ResultNoCaps {
		t.Fatalf("no pluma_cdn: %v, %q", res, result)
	}
	if countries.lookups != before {
		t.Errorf("the country of a client without pluma_cdn was looked up")
	}
}

// TestStoreGate: a delivery never checked, or whose check ran out, is
// unhealthy; one whose month reached MonthlyBytes is out of budget; both
// are read again after their cache.
func TestStoreGate(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	set := testSettings(t)
	d, _ := set.Delivery(testDc)
	g := NewStoreGate(store)
	c := newClock()
	out := func() string {
		t.Helper()
		res, err := g.Out(ctx, d, c.now())
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := out(); res != ResultUnhealthy {
		t.Fatalf("never checked: %q", res)
	}
	ms := c.now().UnixMilli()
	if err := store.PutDeliveryHealth(ctx, &DeliveryHealth{DcId: testDc, HealthyUntil: ms + time.Minute.Milliseconds(), UpdatedAt: ms}); err != nil {
		t.Fatal(err)
	}
	if res := out(); res != ResultUnhealthy {
		t.Fatalf("within the cache of the health: %q", res)
	}
	c.add(healthCacheTTL)
	if res := out(); res != "" {
		t.Fatalf("checked: %q", res)
	}
	c.add(time.Minute)
	if res := out(); res != ResultUnhealthy {
		t.Fatalf("the check ran out: %q", res)
	}

	ms = c.now().UnixMilli()
	if err := store.PutDeliveryHealth(ctx, &DeliveryHealth{DcId: testDc, HealthyUntil: ms + time.Hour.Milliseconds(), UpdatedAt: ms}); err != nil {
		t.Fatal(err)
	}
	c.add(healthCacheTTL)
	if res := out(); res != "" {
		t.Fatalf("checked again: %q", res)
	}
	if err := store.AddUsage(ctx, testDc, UsageMonth(c.now()), d.MonthlyBytes/2, d.MonthlyBytes/2); err != nil {
		t.Fatal(err)
	}
	if res := out(); res != "" {
		t.Fatalf("within the cache of the usage: %q", res)
	}
	c.add(budgetCacheTTL)
	if res := out(); res != ResultBudget {
		t.Fatalf("at the budget: %q", res)
	}

	store.SetErr(errors.New("mysql is down"))
	c.add(budgetCacheTTL)
	if _, err := g.Out(ctx, d, c.now()); err == nil {
		t.Fatal("no error from a failed read")
	}
}

// TestTTLLRU: entries live for ttl; past the limit the least recently used
// goes first; a clock that went back keeps nothing.
func TestTTLLRU(t *testing.T) {
	c := newTTLLRU[int, int](2, time.Minute)
	now := newClock().now()
	c.put(1, 10, now)
	c.put(2, 20, now)
	if v, ok := c.get(1, now); !ok || v != 10 {
		t.Fatalf("get 1: %d, %v", v, ok)
	}
	c.put(3, 30, now) // 2 is the least recent
	if _, ok := c.get(2, now); ok {
		t.Fatal("the least recently used survived")
	}
	if _, ok := c.get(1, now.Add(time.Minute)); ok {
		t.Fatal("an entry outlived its ttl")
	}
	if _, ok := c.get(3, now.Add(-time.Hour)); ok {
		t.Fatal("an entry from the future")
	}
	c.put(4, 40, now)
	if v, ok := c.take(4, now); !ok || v != 40 {
		t.Fatalf("take: %d, %v", v, ok)
	}
	if _, ok := c.get(4, now); ok {
		t.Fatal("a taken entry stayed")
	}
}
