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

//go:build integration

package dao

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcache "github.com/teamgram/marmota/pkg/stores/cache"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/syncx"
)

// gatedCache holds the first load from the database once armed, after the
// load read its rows and before the cache stores them: a slow load that
// overlaps a change of the row, whichever take the reader uses.
type gatedCache struct {
	mcache.BatchCache
	armed   atomic.Bool
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newGatedCache() *gatedCache {
	return &gatedCache{
		BatchCache: mcache.New(itCache, syncx.NewSingleFlight(), cache.NewStat("dialog-gated"), sql.ErrNoRows),
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (g *gatedCache) hold() {
	if g.armed.Load() {
		g.once.Do(func() {
			close(g.entered)
			<-g.release
		})
	}
}

func (g *gatedCache) gate(query func(v any) error) func(v any) error {
	return func(v any) error {
		err := query(v)
		g.hold()
		return err
	}
}

func (g *gatedCache) gateKeys(query func(keys ...string) (map[string]any, error)) func(keys ...string) (map[string]any, error) {
	return func(keys ...string) (map[string]any, error) {
		values, err := query(keys...)
		g.hold()
		return values, err
	}
}

func (g *gatedCache) TakeCtx(ctx context.Context, v any, key string, query func(v any) error) error {
	return g.BatchCache.TakeCtx(ctx, v, key, g.gate(query))
}

func (g *gatedCache) TakeLeasedCtx(ctx context.Context, v any, key string, query func(v any) error) error {
	return g.BatchCache.TakeLeasedCtx(ctx, v, key, g.gate(query))
}

func (g *gatedCache) TakesCtx(ctx context.Context, query func(keys ...string) (map[string]any, error),
	cacheF func(k, v string) (any, error), keys ...string) error {
	return g.BatchCache.TakesCtx(ctx, g.gateKeys(query), cacheF, keys...)
}

func (g *gatedCache) TakesLeasedCtx(ctx context.Context, query func(keys ...string) (map[string]any, error),
	cacheF func(k, v string) (any, error), keys ...string) error {
	return g.BatchCache.TakesLeasedCtx(ctx, g.gateKeys(query), cacheF, keys...)
}

// A read of a dialog row that began before msg changed the row and ends
// after msg deleted its key (the unread mention of a group arriving while
// the client reads its dialogs): the read returns what it read, but a read
// after it gets the committed row, not the one the overlapping read loaded.
func TestDialogLoadOverlappingAChangeDoesNotFillTheCache(t *testing.T) {
	const chatId = 7302
	peerDialogId := mtproto.MakePeerDialogId(mtproto.PEER_CHAT, chatId)
	reads := map[string]func(d *Dao, userId int64) (int32, error){
		"getDialog": func(d *Dao, userId int64) (int32, error) {
			dlg, err := d.GetDialogByPeerDialogId(context.Background(), userId, peerDialogId)
			return dlg.GetDialog().GetUnreadMentionsCount(), err
		},
		"getDialogsByIdList": func(d *Dao, userId int64) (int32, error) {
			list, err := d.GetDialogListByIdList(context.Background(), userId, []int64{peerDialogId})
			if err != nil || len(list) != 1 {
				return -1, err
			}
			return list[0].GetDialog().GetUnreadMentionsCount(), nil
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			userId := newUser()
			pinAddDialog(t, userId, mtproto.PEER_CHAT, chatId, 0)
			key := dialog.GetDialogCacheKey(userId, peerDialogId)

			g := newGatedCache()
			d := *itDao
			d.CachedConn = sqlc.NewConnWithCache(itDao.Mysql.DB, g)
			if err := d.CachedConn.DelCache(ctx, key); err != nil {
				t.Fatal(err)
			}

			g.armed.Store(true)
			var unblock sync.Once
			defer unblock.Do(func() { close(g.release) })
			type result struct {
				mentions int32
				err      error
			}
			stale := make(chan result, 1)
			go func() {
				n, err := read(&d, userId)
				stale <- result{n, err}
			}()
			select {
			case <-g.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the read did not load the row")
			}

			// What msg does when the mention arrives: the box transaction,
			// then the deletion of the key after its commit.
			if err := d.RunBox(ctx, userId, nil, func(b *userbox.Box) error {
				if _, err := b.Tx().Exec("UPDATE dialogs SET unread_mentions_count = 1 WHERE user_id = ? AND peer_dialog_id = ?", userId, peerDialogId); err != nil {
					return err
				}
				b.InvalidateCache(key)
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			unblock.Do(func() { close(g.release) })
			if r := <-stale; r.err != nil || r.mentions != 0 {
				t.Fatalf("the read overlapping the change: %d unread mentions, %v; want 0", r.mentions, r.err)
			}
			if n, err := read(&d, userId); err != nil || n != 1 {
				t.Errorf("a read after the change: %d unread mentions, %v; want 1", n, err)
			}
		})
	}
}
