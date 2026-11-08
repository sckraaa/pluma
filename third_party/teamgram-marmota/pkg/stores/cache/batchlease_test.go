// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/redis/redistest"
	"github.com/zeromicro/go-zero/core/syncx"
)

func leaseCache(t *testing.T) (BatchCache, *redis.Redis) {
	r := redistest.CreateRedis(t)
	return NewNode(r, syncx.NewSingleFlight(), cache.NewStat("test"), errTestNotFound), r
}

func TestLeasedLoadFillsTheCache(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	calls := 0
	load := func(v any) error {
		calls++
		*v.(*int) = 7
		return nil
	}
	for range 2 {
		var got int
		assert.NoError(t, bc.TakeLeasedCtx(ctx, &got, "k", load))
		assert.Equal(t, 7, got)
	}
	assert.Equal(t, 1, calls)
	data, err := r.Get("k")
	assert.NoError(t, err)
	assert.Equal(t, "7", data)
}

// The writer's deletion lands while the load runs: what the load read is
// returned to its caller and not stored.
func TestLeasedLoadOverlappingADeletionDoesNotFill(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	var got int
	assert.NoError(t, bc.TakeLeasedCtx(ctx, &got, "k", func(v any) error {
		assert.NoError(t, bc.DelCtx(ctx, "k"))
		*v.(*int) = 1
		return nil
	}))
	assert.Equal(t, 1, got)
	data, err := r.Get("k")
	assert.NoError(t, err)
	assert.Empty(t, data)

	assert.NoError(t, bc.TakeLeasedCtx(ctx, &got, "k", func(v any) error {
		*v.(*int) = 2
		return nil
	}))
	assert.Equal(t, 2, got)
}

// A value the writer stored meanwhile (a new account) stays.
func TestLeasedLoadDoesNotReplaceAStoredValue(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	var got int
	assert.NoError(t, bc.TakeLeasedCtx(ctx, &got, "k", func(v any) error {
		assert.NoError(t, bc.SetCtx(ctx, "k", 9))
		*v.(*int) = 1
		return nil
	}))
	data, err := r.Get("k")
	assert.NoError(t, err)
	assert.Equal(t, "9", data)
}

// A reader that finds another's lease loads the row itself, leaves the
// lease in place and is not answered with it.
func TestLeasedReaderDuringAnotherLoad(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	var outer int
	assert.NoError(t, bc.TakeLeasedCtx(ctx, &outer, "k", func(v any) error {
		var inner int
		assert.NoError(t, bc.TakeLeasedCtx(ctx, &inner, "k", func(v2 any) error {
			*v2.(*int) = 2
			return nil
		}))
		assert.Equal(t, 2, inner)
		data, err := r.Get("k")
		assert.NoError(t, err)
		assert.True(t, isLease(data), "the key holds %q", data)
		*v.(*int) = 1
		return nil
	}))
	assert.Equal(t, 1, outer)
	data, err := r.Get("k")
	assert.NoError(t, err)
	assert.Equal(t, "1", data)
}

func TestLeasedNotFound(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	calls := 0
	load := func(any) error {
		calls++
		return errTestNotFound
	}
	var got int
	assert.ErrorIs(t, bc.TakeLeasedCtx(ctx, &got, "k", load), errTestNotFound)
	assert.ErrorIs(t, bc.TakeLeasedCtx(ctx, &got, "k", load), errTestNotFound)
	assert.Equal(t, 1, calls)
	data, err := r.Get("k")
	assert.NoError(t, err)
	assert.Equal(t, notFoundPlaceholder, data)
}

// A failed load frees the key at once: the next reader may cache.
func TestLeasedFailedLoadFreesTheKey(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	failure := errors.New("database down")
	var got int
	assert.ErrorIs(t, bc.TakeLeasedCtx(ctx, &got, "k", func(any) error { return failure }), failure)
	data, err := r.Get("k")
	assert.NoError(t, err)
	assert.Empty(t, data)
}

func TestLeasedBatch(t *testing.T) {
	bc, r := leaseCache(t)
	ctx := context.Background()
	assert.NoError(t, bc.Set("k0", 100))
	got := map[string]int{}
	cacheF := func(k, v string) (any, error) {
		n, err := strconv.Atoi(v)
		got[k] = n
		return n, err
	}
	var queried []string
	err := bc.TakesLeasedCtx(ctx, func(keys ...string) (map[string]any, error) {
		queried = keys
		// k2 changes while it loads; k3 does not exist.
		assert.NoError(t, bc.DelCtx(ctx, "k2"))
		return map[string]any{"k1": 1, "k2": 2}, nil
	}, cacheF, "k0", "k1", "k2", "k3")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2", "k3"}, queried)
	assert.Equal(t, map[string]int{"k0": 100}, got)

	want := map[string]string{"k0": "100", "k1": "1", "k2": "", "k3": notFoundPlaceholder}
	for k, v := range want {
		data, err := r.Get(k)
		assert.NoError(t, err)
		assert.Equal(t, v, data, k)
	}
}

func TestLeasedCluster(t *testing.T) {
	r1 := redistest.CreateRedis(t)
	r2 := redistest.CreateRedis(t)
	st := cache.NewStat("test")
	c := cache.ClusterConf{
		{RedisConf: redis.RedisConf{Host: r1.Addr, Type: redis.NodeType}, Weight: 100},
		{RedisConf: redis.RedisConf{Host: r2.Addr, Type: redis.NodeType}, Weight: 100},
	}
	bc := New(c, syncx.NewSingleFlight(), st, errTestNotFound)
	ctx := context.Background()
	for i := range 10 {
		key := "k" + strconv.Itoa(i)
		var got int
		assert.NoError(t, bc.TakeLeasedCtx(ctx, &got, key, func(v any) error {
			*v.(*int) = i + 1
			return nil
		}))
		got = 0
		assert.NoError(t, bc.TakeLeasedCtx(ctx, &got, key, func(any) error {
			t.Errorf("%s was not cached", key)
			return nil
		}))
		assert.Equal(t, i+1, got)
	}
}
