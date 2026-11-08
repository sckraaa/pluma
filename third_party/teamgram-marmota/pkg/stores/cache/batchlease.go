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
	"crypto/rand"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// A load from the database that overlaps a change must not fill the cache:
// it read the row before the commit, the writer deleted the key after it,
// and only then the load would store what it read. The entry would stay
// wrong until it expires.
//
// The leased reads close that window without touching the writers. Who
// misses a key writes a lease into it, a token of its own; the load then
// replaces the lease only if the key still holds that token. The writer's
// deletion removes the lease with the key, so a load that began before the
// deletion finds its lease gone and stores nothing, while one that began
// after it reads the committed row. A reader that finds another's lease
// loads the row too and leaves the cache alone.
//
// The misses are not shared through a singleflight: a request that arrives
// after a commit must not be answered by a load begun before it.

const (
	leasePrefix = "~lease:"
	// leaseExpiry outlasts any load; a lease whose holder died only keeps
	// the key out of the cache until then.
	leaseExpiry = 30 * time.Second

	fillLeaseScript = `if redis.call('GET', KEYS[1]) == ARGV[1] then
	redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
	return 1
end
return 0`
	dropLeaseScript = `if redis.call('GET', KEYS[1]) == ARGV[1] then
	redis.call('DEL', KEYS[1])
	return 1
end
return 0`
)

func isLease(data string) bool { return strings.HasPrefix(data, leasePrefix) }

func (bn *batchNode) TakeLeasedCtx(ctx context.Context, v any, key string, query func(v any) error) error {
	found := false
	err := bn.TakesLeasedCtx(ctx,
		func(keys ...string) (map[string]any, error) {
			if err := query(v); err != nil {
				if errors.Is(err, bn.errNotFound) {
					return nil, nil
				}
				return nil, err
			}
			found = true
			return map[string]any{key: v}, nil
		},
		func(_, data string) (any, error) {
			if err := jsonx.UnmarshalFromString(data, v); err != nil {
				return nil, err
			}
			found = true
			return v, nil
		},
		key)
	if err != nil {
		return err
	}
	if !found {
		return bn.errNotFound
	}
	return nil
}

func (bn *batchNode) TakesLeasedCtx(ctx context.Context,
	query func(keys ...string) (map[string]any, error),
	cacheF func(k, v string) (any, error), keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	logger := logx.WithContext(ctx)
	cmds := make([]*redis.StringCmd, len(keys))
	err := bn.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for i, key := range keys {
			cmds[i] = pipe.Get(ctx, key)
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		logger.Error(err)
		return err
	}

	qKeys := make([]string, 0, len(keys))
	free := make([]string, 0, len(keys))
	for i, cmd := range cmds {
		if data, err := cmd.Result(); err == nil && isLease(data) {
			bn.stat.IncrementTotal()
			bn.stat.IncrementMiss()
			qKeys = append(qKeys, keys[i])
			continue
		}
		if err := bn.doGetCache(ctx, keys[i], cmd, cacheF); err != nil {
			if errors.Is(err, errPlaceholder) || !errors.Is(err, bn.errNotFound) {
				continue
			}
			qKeys = append(qKeys, keys[i])
			free = append(free, keys[i])
		}
	}
	if len(qKeys) == 0 {
		return nil
	}

	token := leasePrefix + rand.Text()
	held := bn.takeLeases(ctx, token, free)

	values, err := query(qKeys...)
	if err != nil {
		bn.stat.IncrementDbFails()
		bn.dropLeases(ctx, token, held)
		return err
	}
	bn.fillLeases(ctx, token, held, values)
	return nil
}

// takeLeases writes token into those of keys that are free and returns
// them. A key it could not lease is loaded all the same, and not cached.
func (bn *batchNode) takeLeases(ctx context.Context, token string, keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	taken := make([]func() (bool, error), len(keys))
	if err := bn.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for i, key := range keys {
			taken[i] = pipe.SetNX(ctx, key, token, leaseExpiry).Result
		}
		return nil
	}); err != nil {
		logx.WithContext(ctx).Error(err)
	}
	held := make([]string, 0, len(keys))
	for i, result := range taken {
		if ok, err := result(); err == nil && ok {
			held = append(held, keys[i])
		}
	}
	return held
}

// fillLeases stores the loaded values into the keys that still hold token.
func (bn *batchNode) fillLeases(ctx context.Context, token string, keys []string, values map[string]any) {
	if len(keys) == 0 {
		return
	}
	logger := logx.WithContext(ctx)
	seconds := int(math.Ceil(bn.aroundDuration(bn.expiry).Seconds()))
	notFoundSeconds := int(math.Ceil(bn.aroundDuration(bn.notFoundExpiry).Seconds()))
	if err := bn.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, key := range keys {
			v, ok := values[key]
			if !ok || v == nil {
				pipe.Eval(ctx, fillLeaseScript, []string{key}, token, notFoundPlaceholder, notFoundSeconds)
				continue
			}
			data, err := jsonx.MarshalToString(v)
			if err != nil {
				logger.Error(err)
				pipe.Eval(ctx, dropLeaseScript, []string{key}, token)
				continue
			}
			pipe.Eval(ctx, fillLeaseScript, []string{key}, token, data, seconds)
		}
		return nil
	}); err != nil {
		logger.Error(err)
	}
}

// dropLeases frees the keys that still hold token after a failed load.
func (bn *batchNode) dropLeases(ctx context.Context, token string, keys []string) {
	if len(keys) == 0 {
		return
	}
	if err := bn.rds.PipelinedCtx(ctx, func(pipe redis.Pipeliner) error {
		for _, key := range keys {
			pipe.Eval(ctx, dropLeaseScript, []string{key}, token)
		}
		return nil
	}); err != nil {
		logx.WithContext(ctx).Error(err)
	}
}
