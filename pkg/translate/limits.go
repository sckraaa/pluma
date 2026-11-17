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

package translate

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// The limits of a request and of a user (ADR 0064, section 7). There are no
// quotas: a user translates as much as he likes. What is bounded is one
// request (CheckItems) and how fast one user can keep the engine busy
// (Limits): only the items that go to the engine count, the cache is free.

// CheckItems checks the texts of one request against the bounds of c (a
// configuration with its defaults in): none is INPUT_TEXT_EMPTY; more than
// MaxItems, an item longer than MaxItemLength, or more than
// MaxRequestLength together is INPUT_TEXT_TOO_LONG. Lengths are UTF-16
// code units, as a client counts message_length_max.
func CheckItems(c Config, items []*mtproto.TextWithEntities) error {
	if len(items) == 0 {
		return mtproto.ErrInputTextEmpty
	}
	if len(items) > c.MaxItems {
		return ErrInputTextTooLong
	}
	total := 0
	for _, it := range items {
		n := utf16Len(it.GetText())
		if n > c.MaxItemLength {
			return ErrInputTextTooLong
		}
		total += n
	}
	if total > c.MaxRequestLength {
		return ErrInputTextTooLong
	}
	return nil
}

// utf16Len is the length of s in UTF-16 code units; an invalid byte counts
// as the one unit of U+FFFD it is read as.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// Limits bounds the items one user sends to the engine.
type Limits interface {
	// Reserve takes n items of userId's budget. It returns nil when they
	// fit, and FLOOD_WAIT_X, taking nothing, when they do not.
	Reserve(ctx context.Context, userId int64, n int) error
}

// The windows of RedisLimits and the prefixes of their counters.
const (
	limitMinuteKey = "translate_items_m_"
	limitHourKey   = "translate_items_h_"
)

// reserveScript takes ARGV[2] from the budget ARGV[3] of the fixed window
// of KEYS[1] (ARGV[1] seconds, started by its first hit). Over the budget
// it takes nothing back out of what was there. Returns {fits, seconds
// left in the window}.
const reserveScript = `
local n = redis.call('INCRBY', KEYS[1], ARGV[2])
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
if n > tonumber(ARGV[3]) then
  n = redis.call('DECRBY', KEYS[1], ARGV[2])
  if n <= 0 then redis.call('DEL', KEYS[1]) end
  return {0, ttl}
end
return {1, ttl}`

// releaseScript gives ARGV[1] back to the counter KEYS[1]; it never
// creates the counter (it may have expired meanwhile) and drops it at
// zero.
const releaseScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local n = redis.call('DECRBY', KEYS[1], ARGV[1])
if n <= 0 then redis.call('DEL', KEYS[1]) end
return n`

// RedisLimits keeps the budgets of the users in Redis, as
// logincode.Limiter keeps its counters: one key with a TTL per user and
// window, shared by every bff instance, nothing to clean up. It does not
// use logincode.Limiter itself, which counts one hit a call and answers
// INTERNAL_SERVER_ERROR when Redis fails: a request takes up to MaxItems
// items at once, and a translation never answers 500, which Android and
// tdesktop would repeat without end (ADR 0064, "Повторы транспорта").
type RedisLimits struct {
	kv               kv.Store
	perMinute, perHr int64
}

// NewRedisLimits returns the limits of c (a configuration with its
// defaults in) kept in store.
func NewRedisLimits(store kv.Store, c Config) *RedisLimits {
	return &RedisLimits{kv: store, perMinute: int64(c.ItemsPerMinute), perHr: int64(c.ItemsPerHour)}
}

var _ Limits = (*RedisLimits)(nil)

// Reserve takes n items from the minute and from the hour of userId. When
// Redis fails, the items are let through, logged and counted
// (translate_limit_errors_total): the limit only guards the engine against
// abuse, the engine bounds its own load by its slots, and a failure of
// Redis must not stop the translation of everyone.
func (l *RedisLimits) Reserve(ctx context.Context, userId int64, n int) error {
	if n <= 0 {
		return nil
	}
	subject := strconv.FormatInt(userId, 10)
	minuteKey, hourKey := limitMinuteKey+subject, limitHourKey+subject
	ok, wait, err := l.take(ctx, minuteKey, time.Minute, n, l.perMinute)
	if err != nil {
		return l.failOpen(ctx, userId, err)
	}
	if !ok {
		return l.over(ctx, userId, n, "minute", wait)
	}
	ok, wait, err = l.take(ctx, hourKey, time.Hour, n, l.perHr)
	if err != nil {
		return l.failOpen(ctx, userId, err)
	}
	if !ok {
		if _, err := l.kv.EvalCtx(ctx, releaseScript, minuteKey, n); err != nil {
			// The minute keeps n more than went to the engine until it
			// ends; nothing else depends on it.
			logx.WithContext(ctx).Errorf("translate: give back %d items of the minute of user %d: %v", n, userId, err)
		}
		return l.over(ctx, userId, n, "hour", wait)
	}
	return nil
}

func (l *RedisLimits) take(ctx context.Context, key string, window time.Duration, n int, limit int64) (bool, time.Duration, error) {
	r, err := l.kv.EvalCtx(ctx, reserveScript, key, int64(window/time.Second), n, limit)
	if err != nil {
		return false, 0, err
	}
	fits, ttl, err := parseReserve(r)
	if err != nil {
		return false, 0, err
	}
	return fits == 1, time.Duration(ttl) * time.Second, nil
}

func (l *RedisLimits) failOpen(ctx context.Context, userId int64, err error) error {
	metricLimitErrors.Inc()
	logx.WithContext(ctx).Errorf("translate: limits of user %d not checked: %v", userId, err)
	return nil
}

// over answers a request over a limit: FLOOD_WAIT_X with the rest of the
// window, kept within [MinFloodWait, MaxFloodWait]. An hour that has long
// to go is answered with the longest wait, not with the whole rest: Web K
// repeats only an X up to 60 by itself (lib/appManagers/apiManager.ts:
// 841-870), and a repeat that is refused costs one Redis call.
func (l *RedisLimits) over(ctx context.Context, userId int64, n int, window string, wait time.Duration) error {
	logx.WithContext(ctx).Infof("translate: user %d over the limit of the %s with %d items, wait %v", userId, window, n, wait)
	return FloodWait(wait)
}

func parseReserve(r any) (fits, ttl int64, err error) {
	a, ok := r.([]any)
	if !ok || len(a) != 2 {
		return 0, 0, fmt.Errorf("translate: limits: unexpected reply %T", r)
	}
	if fits, ok = a[0].(int64); !ok {
		return 0, 0, fmt.Errorf("translate: limits: unexpected reply %T", a[0])
	}
	if ttl, ok = a[1].(int64); !ok {
		return 0, 0, fmt.Errorf("translate: limits: unexpected reply %T", a[1])
	}
	return fits, ttl, nil
}
