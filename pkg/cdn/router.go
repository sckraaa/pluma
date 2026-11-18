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
	"container/list"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Limits of the router (task 107, "Пределы").
const (
	// pickHold is how long a key keeps the delivery it was given while
	// that delivery takes redirects; then the route is walked again, so a
	// preferred delivery that came back gets its keys back.
	pickHold = 10 * time.Minute
	// fallbackWindow: a request of a file without cdn_supported this soon
	// after the file was redirected for the same key is a fallback of the
	// client from the delivery.
	fallbackWindow = 5 * time.Minute
	// routerKeys bounds the keys whose choice and avoided deliveries are
	// kept, and routerRedirects the redirects remembered for the fallback
	// signal; the least recently used go first.
	routerKeys      = 10000
	routerRedirects = 65536
	// budgetCacheTTL: the traffic of a month is read again after it. The
	// origin writes its bytes once a minute, so a fresher read gains
	// nothing.
	budgetCacheTTL = time.Minute
)

// Gate tells whether a delivery takes redirects now. health.go writes the
// health of the deliveries and usage.go their traffic; storeGate reads both
// back.
type Gate interface {
	// Out returns "" if d may take a redirect at now, or the result that
	// keeps it out of the routes: ResultUnhealthy or ResultBudget.
	Out(ctx context.Context, d *Delivery, now time.Time) (string, error)
}

// RedirectObserver learns of every redirect: usage.go estimates the egress
// of the CDN by them (edge_bytes_est).
type RedirectObserver interface {
	Redirected(authKeyID int64, f *File, dc int32, now time.Time)
}

// Router picks the delivery of a redirect (task 107, "Выдачи, маршрут,
// шов"): the record of CDN_ROUTES of the client's country, or the record
// "*" (task 109, "Выдача 202 и маршрут по стране"); in it, the first
// delivery that is healthy, within its budget and not avoided by the key,
// held for the key for pickHold. It also takes the fallback signal: a
// client of the key asked for a file it was just redirected to without
// cdn_supported, so that delivery failed it, and the key avoids it for
// AvoidTTL. The state is in memory, which a restart loses safely: the next
// failure of a client is a fallback again. It is safe for concurrent use.
type Router struct {
	// routes are the records of CDN_ROUTES, their deliveries the
	// preferred first; all is the index of the record "*", and byCountry
	// that of the record of each country it names.
	routes    [][]*Delivery
	all       int
	byCountry map[string]int
	gate      Gate
	avoidTTL  time.Duration

	held      *ttlLRU[int64, heldPick]
	avoided   *ttlLRU[avoidKey, struct{}]
	redirects *ttlLRU[redirectKey, int32]
}

// heldPick is the delivery a key holds and the record it was picked by: a
// client whose country changed (a VPN went on or off) is routed by its new
// record at once.
type heldPick struct {
	route int
	dc    int32
}

type avoidKey struct {
	authKeyID int64
	dc        int32
}

type redirectKey struct {
	authKeyID int64
	file      FileKey
}

// NewRouter returns the Router of set over gate.
func NewRouter(set *Settings, gate Gate) (*Router, error) {
	if len(set.Routes) == 0 {
		return nil, errors.New("cdn: no route")
	}
	r := &Router{
		all:       -1,
		byCountry: map[string]int{},
		gate:      gate,
		avoidTTL:  set.AvoidTTL,
		held:      newTTLLRU[int64, heldPick](routerKeys, pickHold),
		avoided:   newTTLLRU[avoidKey, struct{}](routerKeys, set.AvoidTTL),
		redirects: newTTLLRU[redirectKey, int32](routerRedirects, fallbackWindow),
	}
	for i, route := range set.Routes {
		if len(route.Deliveries) == 0 {
			return nil, errors.New("cdn: a route without deliveries")
		}
		var ds []*Delivery
		for _, id := range route.Deliveries {
			d, ok := set.Delivery(id)
			if !ok {
				return nil, fmt.Errorf("cdn: the route names delivery %d, which is off", id)
			}
			ds = append(ds, d)
		}
		r.routes = append(r.routes, ds)
		if route.Countries == nil {
			if r.all >= 0 {
				return nil, errors.New("cdn: two routes for *")
			}
			r.all = i
			continue
		}
		for _, c := range route.Countries {
			if _, ok := r.byCountry[c]; ok {
				return nil, fmt.Errorf("cdn: two routes for %s", c)
			}
			r.byCountry[c] = i
		}
	}
	if r.all < 0 {
		return nil, errors.New("cdn: no route for *")
	}
	return r, nil
}

// ByCountry reports whether a record of the routes names countries: only
// then is the country of a client looked up.
func (r *Router) ByCountry() bool { return len(r.byCountry) > 0 }

// First is the preferred delivery of the record "*": the one the metrics
// name when no delivery was picked before the client's country was known.
func (r *Router) First() *Delivery { return r.routes[r.all][0] }

// FirstOf is the preferred delivery of the record of country: the one the
// metrics name when no delivery was picked for a client of country.
func (r *Router) FirstOf(country string) *Delivery { return r.routes[r.route(country)][0] }

// route is the index of the record of country, "" being not known.
func (r *Router) route(country string) int {
	if i, ok := r.byCountry[country]; ok {
		return i
	}
	return r.all
}

// Targets are the cdn_replicas targets the deliveries of the routes serve
// from, each once.
func (r *Router) Targets() []int32 {
	var ts []int32
	for _, route := range r.routes {
		for _, d := range route {
			if t := d.Target(); !slices.Contains(ts, t) {
				ts = append(ts, t)
			}
		}
	}
	return ts
}

// Pick returns the delivery a client of authKeyID from country (an ISO
// 3166-1 alpha-2 code, "" when it is not known) is redirected to at now.
// With none, it returns nil and the result that kept out the preferred
// delivery of its record: ResultAvoided, ResultUnhealthy or ResultBudget;
// or ResultError and the failure of the gate.
func (r *Router) Pick(ctx context.Context, authKeyID int64, country string, now time.Time) (*Delivery, string, error) {
	ri := r.route(country)
	route := r.routes[ri]
	if h, ok := r.held.get(authKeyID, now); ok && h.route == ri {
		for _, d := range route {
			if d.ID != h.dc {
				continue
			}
			out, err := r.out(ctx, authKeyID, d, now)
			if err != nil {
				return nil, ResultError, err
			}
			if out == "" {
				return d, "", nil
			}
		}
	}
	first := ""
	for _, d := range route {
		out, err := r.out(ctx, authKeyID, d, now)
		if err != nil {
			return nil, ResultError, err
		}
		if out == "" {
			r.held.put(authKeyID, heldPick{route: ri, dc: d.ID}, now)
			return d, "", nil
		}
		if first == "" {
			first = out
		}
	}
	r.held.remove(authKeyID)
	return nil, first, nil
}

// out is why d takes no redirect of authKeyID at now, "" if it does.
func (r *Router) out(ctx context.Context, authKeyID int64, d *Delivery, now time.Time) (string, error) {
	if _, ok := r.avoided.get(avoidKey{authKeyID, d.ID}, now); ok {
		return ResultAvoided, nil
	}
	return r.gate.Out(ctx, d, now)
}

// Redirected records that a client of authKeyID was redirected to dc for
// the file of key at now.
func (r *Router) Redirected(authKeyID int64, key FileKey, dc int32, now time.Time) {
	r.redirects.put(redirectKey{authKeyID, key}, dc, now)
}

// FellBack takes a request of the file of key by a client of authKeyID
// without cdn_supported at now. If the file was redirected for that key
// within fallbackWindow, the client gave up on the delivery (task 107,
// client contract p. 7-8): the key avoids it for AvoidTTL, and FellBack
// returns its id and true. Each redirect counts once.
func (r *Router) FellBack(authKeyID int64, key FileKey, now time.Time) (int32, bool) {
	dc, ok := r.redirects.take(redirectKey{authKeyID, key}, now)
	if !ok {
		return 0, false
	}
	r.avoided.put(avoidKey{authKeyID, dc}, struct{}{}, now)
	if held, ok := r.held.get(authKeyID, now); ok && held.dc == dc {
		r.held.remove(authKeyID)
	}
	return dc, true
}

// storeGate is the Gate of the health and the traffic the Store keeps:
// health.go writes cdn_deliveries, usage.go cdn_delivery_usage. Both are
// cached briefly per delivery, so a client asking a file a megabyte at a
// time does not query MySQL every time.
type storeGate struct {
	store Store

	mu     sync.Mutex
	health map[int32]healthRead
	usage  map[int32]usageRead
}

type healthRead struct {
	h  *DeliveryHealth // nil: never checked
	at time.Time
}

type usageRead struct {
	u  DeliveryUsage
	at time.Time
}

// NewStoreGate returns the Gate over the health and the usage in store.
func NewStoreGate(store Store) Gate {
	return &storeGate{store: store, health: map[int32]healthRead{}, usage: map[int32]usageRead{}}
}

func (g *storeGate) Out(ctx context.Context, d *Delivery, now time.Time) (string, error) {
	h, err := g.readHealth(ctx, d.ID, now)
	if err != nil {
		return ResultError, err
	}
	if h == nil || h.HealthyUntil <= now.UnixMilli() {
		return ResultUnhealthy, nil
	}
	u, err := g.readUsage(ctx, d.ID, now)
	if err != nil {
		return ResultError, err
	}
	// The month of Gcore ends on the 1st (docs.gcore.com, how the CDN
	// service is billed); its time zone is not documented, so UTC is
	// taken and the default budget keeps 200 GB of margin to the 1 TB.
	if u.OriginBytes+u.EdgeBytesEst >= d.MonthlyBytes {
		return ResultBudget, nil
	}
	return "", nil
}

func fresh(at, now time.Time, ttl time.Duration) bool {
	return !at.IsZero() && !now.Before(at) && now.Sub(at) < ttl
}

func (g *storeGate) readHealth(ctx context.Context, dc int32, now time.Time) (*DeliveryHealth, error) {
	g.mu.Lock()
	r, ok := g.health[dc]
	g.mu.Unlock()
	if ok && fresh(r.at, now, healthCacheTTL) {
		return r.h, nil
	}
	h, err := g.store.DeliveryHealth(ctx, dc)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.health[dc] = healthRead{h: h, at: now}
	g.mu.Unlock()
	return h, nil
}

func (g *storeGate) readUsage(ctx context.Context, dc int32, now time.Time) (DeliveryUsage, error) {
	month := UsageMonth(now)
	g.mu.Lock()
	r, ok := g.usage[dc]
	g.mu.Unlock()
	if ok && r.u.Month == month && fresh(r.at, now, budgetCacheTTL) {
		return r.u, nil
	}
	u, err := g.store.Usage(ctx, dc, month)
	if err != nil {
		return DeliveryUsage{}, err
	}
	u.Month = month
	g.mu.Lock()
	g.usage[dc] = usageRead{u: u, at: now}
	g.mu.Unlock()
	return u, nil
}

// ttlLRU is a map of at most limit entries, each kept for ttl from when it
// was put; past limit the least recently used goes first.
type ttlLRU[K comparable, V any] struct {
	mu    sync.Mutex
	limit int
	ttl   time.Duration
	order *list.List // of *ttlEntry[K, V], the most recent first
	items map[K]*list.Element
}

type ttlEntry[K comparable, V any] struct {
	key   K
	val   V
	until time.Time
}

func newTTLLRU[K comparable, V any](limit int, ttl time.Duration) *ttlLRU[K, V] {
	return &ttlLRU[K, V]{limit: limit, ttl: ttl, order: list.New(), items: map[K]*list.Element{}}
}

// get returns the value of k if it is still kept at now.
func (c *ttlLRU[K, V]) get(k K, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookup(k, now)
}

// take returns the value of k if it is still kept at now, and removes it.
func (c *ttlLRU[K, V]) take(k K, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.lookup(k, now)
	if ok {
		c.drop(c.items[k])
	}
	return v, ok
}

// lookup is get with c.mu held.
func (c *ttlLRU[K, V]) lookup(k K, now time.Time) (V, bool) {
	var zero V
	el, ok := c.items[k]
	if !ok {
		return zero, false
	}
	e := el.Value.(*ttlEntry[K, V])
	// A clock that went back keeps nothing it cannot date.
	if !now.Before(e.until) || e.until.Sub(now) > c.ttl {
		c.drop(el)
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.val, true
}

// put keeps v for k from now for ttl.
func (c *ttlLRU[K, V]) put(k K, v V, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.drop(el)
	}
	c.items[k] = c.order.PushFront(&ttlEntry[K, V]{key: k, val: v, until: now.Add(c.ttl)})
	for len(c.items) > c.limit {
		c.drop(c.order.Back())
	}
}

func (c *ttlLRU[K, V]) remove(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.drop(el)
	}
}

// drop removes el; c.mu is held.
func (c *ttlLRU[K, V]) drop(el *list.Element) {
	c.order.Remove(el)
	delete(c.items, el.Value.(*ttlEntry[K, V]).key)
}
