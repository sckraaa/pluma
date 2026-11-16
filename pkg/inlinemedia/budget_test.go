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
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func newTestKV(t *testing.T) (*miniredis.Miniredis, kv.Store) {
	t.Helper()
	mr := miniredis.RunT(t)
	return mr, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

// The budget takes what fits and nothing of what does not, counts per UTC
// day, and the counter of a day expires.
func TestBudget(t *testing.T) {
	mr, store := newTestKV(t)
	ctx := context.Background()
	b := NewBudget(store, 100)
	now := time.Date(2026, 11, 19, 23, 59, 0, 0, time.FixedZone("UTC+3", 3*3600))
	b.now = func() time.Time { return now }

	for _, tc := range []struct {
		n     int64
		ok    bool
		spent bool
	}{{60, true, false}, {50, false, false}, {40, true, true}, {1, false, true}} {
		ok, err := b.Take(ctx, tc.n)
		if err != nil || ok != tc.ok {
			t.Fatalf("take %d: %v, %v; want %v", tc.n, ok, err, tc.ok)
		}
		spent, err := b.Spent(ctx)
		if err != nil || spent != tc.spent {
			t.Fatalf("after %d: spent %v, %v; want %v", tc.n, spent, err, tc.spent)
		}
	}
	if v, err := mr.Get("inline_import_bytes_20261119"); err != nil || v != "100" {
		t.Fatalf("counter %q, %v", v, err)
	}
	if ttl := mr.TTL("inline_import_bytes_20261119"); ttl <= 24*time.Hour {
		t.Errorf("counter expires in %v", ttl)
	}
	// The next UTC day has the whole budget.
	now = now.Add(24 * time.Hour)
	if ok, err := b.Take(ctx, 100); err != nil || !ok {
		t.Fatalf("next day: %v, %v", ok, err)
	}
}

func TestParseDailyBytes(t *testing.T) {
	for in, want := range map[string]int64{"": DefaultDailyBytes, " ": DefaultDailyBytes, "1048576": 1 << 20} {
		if got, err := ParseDailyBytes(in); err != nil || got != want {
			t.Errorf("%q: %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"0", "-1", "1G", "1.5"} {
		if _, err := ParseDailyBytes(in); err == nil {
			t.Errorf("%q taken", in)
		}
	}
}
