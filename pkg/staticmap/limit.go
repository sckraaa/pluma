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

package staticmap

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

const limitKeyPrefix = "webmap_rl:"

// limitWindow is the window of the per-user limit: a calendar minute.
const limitWindow = time.Minute

// KEYS[1] counter of a user and a minute; ARGV[1] its lifetime in seconds.
// The first hit sets the lifetime; a counter that somehow has none gets it.
// Returns the count.
const limitScript = `
local n = redis.call('INCR', KEYS[1])
if redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return n`

// limiter counts the maps a user has rendered (cache misses) per calendar
// minute in Redis, shared by every bff. The key carries the minute, so a
// counter is never reset, only left to expire.
type limiter struct {
	kv    kv.Store
	limit int64
}

// hit counts one render of userId at now. It returns whether the user is
// over the limit and, if so, the seconds left in the minute (at least 1).
func (l *limiter) hit(ctx context.Context, userId int64, now time.Time) (over bool, wait int32, err error) {
	minute := now.Unix() / int64(limitWindow/time.Second)
	key := limitKeyPrefix + strconv.FormatInt(userId, 10) + ":" + strconv.FormatInt(minute, 10)
	// Two windows of lifetime: a bff whose clock is a little behind still
	// finds the counter.
	r, err := l.kv.EvalCtx(ctx, limitScript, key, int64(2*limitWindow/time.Second))
	if err != nil {
		return false, 0, err
	}
	n, ok := r.(int64)
	if !ok {
		return false, 0, fmt.Errorf("staticmap: unexpected limit reply %T", r)
	}
	if n <= l.limit {
		return false, 0, nil
	}
	left := int64(limitWindow/time.Second) - now.Unix()%int64(limitWindow/time.Second)
	return true, int32(max(left, 1)), nil
}
