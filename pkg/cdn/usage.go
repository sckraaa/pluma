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
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// Rhythm and bounds of the budget (task 107, "Здоровье и бюджет").
const (
	// usageFlushEvery is how often the counted traffic is added to
	// cdn_delivery_usage and the month's totals read back.
	usageFlushEvery = time.Minute
	// usageFlushTimeout bounds a flush, the last one at the stop among
	// them.
	usageFlushTimeout = 10 * time.Second
	// edgeSeenFor: a key that fetches a file again within it is taken to be
	// served by the client's own cache or by the CDN's from the same
	// download, and is not counted again.
	edgeSeenFor = 24 * time.Hour
	// maxEdgeSeen bounds the pairs remembered; the least recent go first,
	// and a pair forgotten early is counted again (an overestimate, the
	// safe side of a budget).
	maxEdgeSeen = 1 << 16
)

// Usage keeps the traffic of the deliveries in a calendar month (UTC) and
// takes a delivery out once it reaches its budget (Delivery.MonthlyBytes):
// what the home origin served for it, exactly, plus the home's estimate of
// what its CDN sent to clients (the size of a file at the first redirect
// of a key to it in a day). The counts are kept in memory and added to
// cdn_delivery_usage every usageFlushEvery; the totals of the month are
// read back then, so several processes count into one row. It is safe for
// concurrent use.
//
// The month of Gcore is reset on the 1st; its time zone is not stated
// (https://docs.gcore.com/cdn/how-the-cdn-service-and-its-additional-options-are-billed.md),
// and UTC is taken: the 200 GB between the default budget and the free
// terabyte cover the hours either way.
type Usage struct {
	store  Store
	now    func() time.Time
	limits map[int32]int64 // by delivery

	mu sync.Mutex
	// pending is counted and not yet added to the table, by delivery and
	// month.
	pending map[usageKey]*DeliveryUsage
	// stored are the totals of the table as last read, by delivery and
	// month; only the month of the last flush is kept.
	stored map[usageKey]DeliveryUsage

	seen *edgeSeen
}

// NewUsage returns the budget of deliveries over store.
func NewUsage(store Store, deliveries []Delivery) *Usage {
	u := &Usage{
		store:   store,
		now:     time.Now,
		limits:  map[int32]int64{},
		pending: map[usageKey]*DeliveryUsage{},
		stored:  map[usageKey]DeliveryUsage{},
		seen:    newEdgeSeen(maxEdgeSeen, edgeSeenFor),
	}
	for _, d := range deliveries {
		u.limits[d.ID] = d.MonthlyBytes
		metricUsageLimit.Set(float64(d.MonthlyBytes), dcLabel(d.ID))
	}
	return u
}

// CountOrigin counts n bytes the origin served for delivery dc.
func (u *Usage) CountOrigin(dc int32, n int64) {
	if n <= 0 {
		return
	}
	u.add(dc, n, 0)
}

// NoteRedirect counts a redirect of the key authKeyId to the file of
// fileRowId, of size bytes, through delivery dc: the size is added to the
// estimate of the CDN's traffic unless the key was sent to that file
// through dc within a day.
func (u *Usage) NoteRedirect(dc int32, authKeyId, fileRowId, size int64) {
	if size <= 0 || !u.seen.first(edgePair{dc: dc, authKeyId: authKeyId, fileRowId: fileRowId}, u.now()) {
		return
	}
	u.add(dc, 0, size)
}

// Redirected is the RedirectObserver of Home: it notes the redirect of the
// key authKeyID to f through delivery dc (NoteRedirect).
func (u *Usage) Redirected(authKeyID int64, f *File, dc int32, _ time.Time) {
	u.NoteRedirect(dc, authKeyID, f.RowId, f.Size)
}

func (u *Usage) add(dc int32, origin, edge int64) {
	k := usageKey{dcId: dc, month: UsageMonth(u.now())}
	u.mu.Lock()
	defer u.mu.Unlock()
	p, ok := u.pending[k]
	if !ok {
		p = &DeliveryUsage{DcId: dc, Month: k.month}
		u.pending[k] = p
	}
	p.OriginBytes += origin
	p.EdgeBytesEst += edge
}

// OverBudget reports whether delivery dc reached its budget this month:
// the totals as last read plus what was counted since. A delivery the
// budget does not know has none.
func (u *Usage) OverBudget(dc int32) bool {
	limit, ok := u.limits[dc]
	if !ok || limit <= 0 {
		return false
	}
	o, e := u.total(dc, UsageMonth(u.now()))
	return o+e >= limit
}

// total is the traffic of dc in month: stored and pending.
func (u *Usage) total(dc int32, month string) (origin, edge int64) {
	k := usageKey{dcId: dc, month: month}
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.stored[k]
	origin, edge = s.OriginBytes, s.EdgeBytesEst
	if p, ok := u.pending[k]; ok {
		origin += p.OriginBytes
		edge += p.EdgeBytesEst
	}
	return origin, edge
}

// Flush adds what was counted to the table and reads the totals of the
// month back. Counts whose addition failed are kept for the next flush: an
// addition that failed after MySQL applied it is counted twice, an
// overestimate.
func (u *Usage) Flush(ctx context.Context) error {
	u.mu.Lock()
	pending := u.pending
	u.pending = map[usageKey]*DeliveryUsage{}
	u.mu.Unlock()

	var errs []error
	for k, p := range pending {
		if p.OriginBytes == 0 && p.EdgeBytesEst == 0 {
			continue
		}
		if err := u.store.AddUsage(ctx, k.dcId, k.month, p.OriginBytes, p.EdgeBytesEst); err != nil {
			errs = append(errs, fmt.Errorf("delivery %d, %s: %w", k.dcId, k.month, err))
			u.mu.Lock()
			q, ok := u.pending[k]
			if !ok {
				q = &DeliveryUsage{DcId: k.dcId, Month: k.month}
				u.pending[k] = q
			}
			q.OriginBytes += p.OriginBytes
			q.EdgeBytesEst += p.EdgeBytesEst
			u.mu.Unlock()
		}
	}

	month := UsageMonth(u.now())
	stored := map[usageKey]DeliveryUsage{}
	for dc := range u.limits {
		got, err := u.store.Usage(ctx, dc, month)
		if err != nil {
			errs = append(errs, fmt.Errorf("delivery %d, %s: %w", dc, month, err))
			// The last totals stay: a month without them counts from the
			// pending bytes alone.
			u.mu.Lock()
			if s, ok := u.stored[usageKey{dcId: dc, month: month}]; ok {
				stored[usageKey{dcId: dc, month: month}] = s
			}
			u.mu.Unlock()
			continue
		}
		stored[usageKey{dcId: dc, month: month}] = got
	}
	u.mu.Lock()
	u.stored = stored
	u.mu.Unlock()
	for dc := range u.limits {
		o, e := u.total(dc, month)
		metricUsage.Set(float64(o), dcLabel(dc), usageKindOrigin)
		metricUsage.Set(float64(e), dcLabel(dc), usageKindEdgeEst)
	}
	return errors.Join(errs...)
}

// Run flushes now, then every usageFlushEvery until ctx is done, and a
// last time after that.
func (u *Usage) Run(ctx context.Context) {
	t := time.NewTicker(usageFlushEvery)
	defer t.Stop()
	for {
		u.flush(ctx)
		select {
		case <-ctx.Done():
			// The last flush outlives ctx: what the origin served until
			// the stop is not lost.
			u.flush(context.WithoutCancel(ctx))
			return
		case <-t.C:
		}
	}
}

func (u *Usage) flush(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, usageFlushTimeout)
	defer cancel()
	if err := u.Flush(ctx); err != nil {
		logx.WithContext(ctx).Errorf("cdn: traffic of the deliveries: %v", err)
	}
}

// edgePair is a key that was sent to a file through a delivery.
type edgePair struct {
	dc        int32
	authKeyId int64
	fileRowId int64
}

// edgeSeen remembers the pairs of the last ttl, at most max of them.
type edgeSeen struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	order *list.List // of *edgeEntry, the most recent first
	byKey map[edgePair]*list.Element
}

type edgeEntry struct {
	pair edgePair
	at   time.Time
}

func newEdgeSeen(maxPairs int, ttl time.Duration) *edgeSeen {
	return &edgeSeen{max: maxPairs, ttl: ttl, order: list.New(), byKey: map[edgePair]*list.Element{}}
}

// first reports whether p was not seen within ttl of now, and remembers it
// at now if so.
func (s *edgeSeen) first(p edgePair, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.byKey[p]; ok {
		e := el.Value.(*edgeEntry)
		if age := now.Sub(e.at); age >= 0 && age < s.ttl {
			return false
		}
		e.at = now
		s.order.MoveToFront(el)
		return true
	}
	s.byKey[p] = s.order.PushFront(&edgeEntry{pair: p, at: now})
	for s.order.Len() > s.max {
		old := s.order.Back()
		s.order.Remove(old)
		delete(s.byKey, old.Value.(*edgeEntry).pair)
	}
	return true
}
