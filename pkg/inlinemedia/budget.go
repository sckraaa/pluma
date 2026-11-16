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

package inlinemedia

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// DefaultDailyBytes is the import budget when INLINE_IMPORT_DAILY_BYTES is
// empty: 1 GiB a UTC day (ADR 0062).
const DefaultDailyBytes = 1 << 30

// ParseDailyBytes reads INLINE_IMPORT_DAILY_BYTES: empty is
// DefaultDailyBytes, otherwise a positive number of bytes.
func ParseDailyBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultDailyBytes, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("inlinemedia: INLINE_IMPORT_DAILY_BYTES %q is not a positive number of bytes", s)
	}
	return n, nil
}

// budgetKeep is how long the counter of a day stays in Redis: past the end
// of the day, whatever the clock of the replica that made it.
const budgetKeep = 2 * 24 * 3600

// takeScript takes ARGV[1] bytes of the day's budget ARGV[2] if they fit:
// the new total, or -1 when they do not and nothing was taken. KEYS[1] is
// the counter of the day.
const takeScript = `
local spent = tonumber(redis.call('GET', KEYS[1]) or '0')
local n = tonumber(ARGV[1])
if spent + n > tonumber(ARGV[2]) then return -1 end
spent = redis.call('INCRBY', KEYS[1], n)
redis.call('EXPIRE', KEYS[1], ARGV[3])
return spent`

// Budget is how many bytes the imports may store in a UTC day, counted in
// Redis (inline_import_bytes_<yyyymmdd>) for every replica of bff: past it
// only media imported before are served. A counter lost with Redis starts
// the day again; the budget bounds the growth of dfs, not a bill.
type Budget struct {
	kv    kv.Store
	limit int64
	now   func() time.Time
}

// NewBudget returns the Budget of limit bytes a day.
func NewBudget(store kv.Store, limit int64) *Budget {
	return &Budget{kv: store, limit: limit, now: time.Now}
}

func (b *Budget) key() string {
	return "inline_import_bytes_" + b.now().UTC().Format("20060102")
}

// Spent reports whether nothing more fits today: an import does not even
// fetch then.
func (b *Budget) Spent(ctx context.Context) (bool, error) {
	s, err := b.kv.GetCtx(ctx, b.key())
	if errors.Is(err, redis.Nil) || err == nil && s == "" {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inlinemedia: budget: %w", err)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return false, fmt.Errorf("inlinemedia: budget %q: %w", s, err)
	}
	return n >= b.limit, nil
}

// Take takes n bytes of today's budget: false, and nothing taken, when they
// do not fit.
func (b *Budget) Take(ctx context.Context, n int64) (bool, error) {
	r, err := b.kv.EvalCtx(ctx, takeScript, b.key(), n, b.limit, budgetKeep)
	if err != nil {
		return false, fmt.Errorf("inlinemedia: budget: %w", err)
	}
	v, ok := r.(int64)
	if !ok {
		return false, fmt.Errorf("inlinemedia: budget: reply %T", r)
	}
	return v >= 0, nil
}
