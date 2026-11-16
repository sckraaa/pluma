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

package svc

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

// The items bots prepare for mini apps (bot_prepared, task 106) expire after
// a day and nobody asks for them again: a background round deletes the
// expired ones every preparedPurgeEvery (the brief: once an hour), the first
// at the start. Every bff process runs it; two rounds at once only delete
// the same rows, a batch at a time (botstore.PurgeExpiredPrepared).
const (
	preparedPurgeEvery   = time.Hour
	preparedPurgeTimeout = 5 * time.Minute
)

// preparedPurger deletes the prepared items expired at now
// (*botstore.Store).
type preparedPurger interface {
	PurgeExpiredPrepared(ctx context.Context, now int32) (int64, error)
}

// purgePreparedEvery runs purgePrepared now and then every period until ctx
// ends.
func purgePreparedEvery(ctx context.Context, store preparedPurger, period time.Duration) {
	threading.GoSafe(func() {
		tick := time.NewTicker(period)
		defer tick.Stop()
		for {
			purgePrepared(ctx, store)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
}

// purgePrepared is one round. A failed round is logged: the rows stay, and
// expired ones are never served (botstore.PreparedOf), so the next round
// takes them.
func purgePrepared(ctx context.Context, store preparedPurger) {
	ctx, cancel := context.WithTimeout(ctx, preparedPurgeTimeout)
	defer cancel()
	n, err := store.PurgeExpiredPrepared(ctx, int32(time.Now().Unix()))
	if err != nil {
		logx.WithContext(ctx).Errorf("bot_prepared: purge after %d rows: %v", n, err)
		return
	}
	if n > 0 {
		logx.WithContext(ctx).Infof("bot_prepared: %d expired rows deleted", n)
	}
}
