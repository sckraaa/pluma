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

package msgsearch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// PostsDaily is how many different queries of channels.searchPosts a user
// may run in a UTC day (searchPostsFlood.total_daily): the search of all
// public channels is the heaviest one, and there is nothing to pay for it
// with here.
const PostsDaily = 100

// PostsDB reads and writes the slots of the daily quota.
type PostsDB interface {
	Querier
	Execer
}

func dayOf(t time.Time) int32 { return int32(t.UTC().Unix() / 86400) }

func secondsToDayEnd(t time.Time) int32 {
	return (dayOf(t)+1)*86400 - int32(t.UTC().Unix())
}

// queryHash identifies a query in the quota without keeping its text.
func queryHash(query string) int64 {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(query))))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// PostsFlood is the state of the user's quota, for query when not empty:
// query_is_free when it was already searched today (searching it again
// takes no slot, and with no slot left costs nothing).
// wait_till says when the quota renews when none is left.
func PostsFlood(ctx context.Context, db Querier, userId int64, query string, now time.Time) (*mtproto.SearchPostsFlood, error) {
	day := dayOf(now)
	var hashes []int64
	if err := db.QueryRowsPartial(ctx, &hashes, "SELECT query_hash FROM posts_search_slots WHERE user_id = ? AND day = ?", userId, day); err != nil {
		return nil, err
	}
	remains := max(int32(PostsDaily-len(hashes)), 0)
	free := false
	if query != "" {
		h := queryHash(query)
		for _, v := range hashes {
			free = free || v == h
		}
	}
	return makeFlood(free, remains, now), nil
}

func makeFlood(free bool, remains int32, now time.Time) *mtproto.SearchPostsFlood {
	f := &mtproto.SearchPostsFlood{
		QueryIsFree: free,
		TotalDaily:  PostsDaily,
		Remains:     remains,
		StarsAmount: 0,
	}
	if remains == 0 {
		f.WaitTill = mtproto.MakeFlagsInt32((dayOf(now) + 1) * 86400)
	}
	return mtproto.MakeTLSearchPostsFlood(f).To_SearchPostsFlood()
}

// TakePostsSlot takes a slot of the quota for a first page of query and
// says how the quota stands after it. An already searched query takes
// none; with none left the error is FLOOD_WAIT_X for the rest of the day,
// which clients answer by refreshing the state (tdesktop
// dialogs_search_posts.cpp).
func TakePostsSlot(ctx context.Context, db PostsDB, userId int64, query string, now time.Time) (*mtproto.SearchPostsFlood, error) {
	day := dayOf(now)
	h := queryHash(query)
	if _, err := db.Exec(ctx, "DELETE FROM posts_search_slots WHERE user_id = ? AND day < ?", userId, day); err != nil {
		return nil, err
	}
	var had []int64
	if err := db.QueryRowsPartial(ctx, &had, "SELECT query_hash FROM posts_search_slots WHERE user_id = ? AND day = ? AND query_hash = ?", userId, day, h); err != nil {
		return nil, err
	}
	if len(had) == 0 {
		// One statement: a burst of parallel searches goes over the limit
		// by at most their number, which is of no consequence.
		res, err := db.Exec(ctx,
			"INSERT IGNORE INTO posts_search_slots (user_id, day, query_hash) SELECT ?, ?, ? FROM DUAL"+
				" WHERE (SELECT COUNT(*) FROM posts_search_slots WHERE user_id = ? AND day = ?) < ?",
			userId, day, h, userId, day, PostsDaily)
		if err != nil {
			return nil, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if n == 0 {
			return nil, mtproto.NewErrFloodWaitX(secondsToDayEnd(now))
		}
	}
	return PostsFlood(ctx, db, userId, query, now)
}
