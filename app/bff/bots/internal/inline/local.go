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

package inline

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/singleflight"
)

// The popular GIFs of @gif without a search provider (task 099, part D,
// ADR 0062).
const (
	// localPageSize is the GIFs of a page.
	localPageSize = 20
	// localPopularMax is the most GIFs the popular list has.
	localPopularMax = 200
	// localSaversMin is how many different people must have saved a GIF
	// for it to be popular: a GIF one person saved is not shown to others
	// (decision 2 of the brief).
	localSaversMin = 3
	// localSendsDays is the days of sends from @gif's results that count,
	// today (UTC) included.
	localSendsDays = 30
	// localCacheTime is the cache_time of every page, and how long the
	// popular list is kept in Redis.
	localCacheTime = 600
	// localLoadWait bounds a load of the popular list from MySQL, which
	// the queries waiting for it share.
	localLoadWait = 5 * time.Second
	// localPopularKey is the Redis key of the popular list; the value is
	// localPopularTag and the ids, comma separated, the most popular first.
	localPopularKey = "inline_local_popular"
	localPopularTag = "v1:"
	// localSendsDeleteBatch bounds the old rows of inline_gif_sends one
	// load deletes.
	localSendsDeleteBatch = 1000
)

// PopularGifs counts the popular GIFs (*GifCounts).
type PopularGifs interface {
	// PopularGifs returns the ids of at most limit popular documents, the
	// most popular first, as of now.
	PopularGifs(ctx context.Context, now time.Time, limit int) ([]int64, error)
}

// Documents has the documents of ids (media_client.MediaClient). A
// document that is gone is missing from the answer or documentEmpty, and
// the answer is in no particular order.
type Documents interface {
	MediaGetDocumentList(ctx context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error)
}

// Cache keeps the popular list between the queries and the replicas of
// the service (kv.Store). A key that is not there reads as "".
type Cache interface {
	GetCtx(ctx context.Context, key string) (string, error)
	SetexCtx(ctx context.Context, key, value string, seconds int) error
}

// Local is the provider of @gif while it has no search: the GIFs popular
// within the server for the empty query, which the GIF tab of the emoji
// panel sends (Android EmojiView.java:8939; iOS GifContext.swift:66), and
// nothing for a text. Its media are documents already on the server, so
// nothing is imported.
type Local struct {
	counts PopularGifs
	docs   Documents
	cache  Cache
	now    func() time.Time
	loads  singleflight.Group
}

// NewLocal returns the provider on counts, docs and cache.
func NewLocal(counts PopularGifs, docs Documents, cache Cache) *Local {
	return &Local{counts: counts, docs: docs, cache: cache, now: time.Now}
}

// Search answers q. The pages walk the popular list: the offset is the
// index in it of the GIF the page starts with. A text, or an offset this
// provider did not give, is an empty page; every page may be served again
// for localCacheTime, as long as the list itself is kept.
func (l *Local) Search(ctx context.Context, q Query) (Page, error) {
	page := Page{Items: []Item{}, CacheTime: localCacheTime}
	if q.Text != "" {
		return page, nil
	}
	from, ok := parseLocalOffset(q.Offset)
	if !ok {
		return page, nil
	}
	ids, err := l.popular(ctx)
	if err != nil {
		return Page{}, err
	}
	at := from
	for at < len(ids) && len(page.Items) < localPageSize {
		end := min(at+localPageSize-len(page.Items), len(ids))
		docs, err := l.docs.MediaGetDocumentList(ctx, &media.TLMediaGetDocumentList{IdList: ids[at:end]})
		if err != nil {
			return Page{}, fmt.Errorf("inline: local documents: %w", err)
		}
		byId := make(map[int64]*mtproto.Document, len(docs.GetDatas()))
		for _, d := range docs.GetDatas() {
			if isGif(d) {
				byId[d.GetId()] = d
			}
		}
		for i := at; i < end; i++ {
			// A GIF deleted since it was counted, or a document clients
			// would not show as a GIF, is skipped; the page takes the next
			// ones in its place.
			if d := byId[ids[i]]; d != nil {
				page.Items = append(page.Items, Item{Document: d, Next: localOffset(i+1, len(ids))})
			}
		}
		at = end
	}
	page.Next = localOffset(at, len(ids))
	return page, nil
}

// popular returns the popular list: from Redis, or counted again and kept
// there. A Redis that fails is logged and the list counted again, as the
// answer does not need the cache.
func (l *Local) popular(ctx context.Context) ([]int64, error) {
	v, err := l.cache.GetCtx(ctx, localPopularKey)
	if err != nil {
		logx.WithContext(ctx).Errorf("inline: local popular cache: %v", err)
	} else if ids, ok := decodeLocalIds(v); ok {
		return ids, nil
	}
	got := l.loads.DoChan(localPopularKey, func() (interface{}, error) {
		// The load is shared by the queries that wait for it: the one that
		// started it going away must not fail the others.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), localLoadWait)
		defer cancel()
		ids, err := l.counts.PopularGifs(lctx, l.now(), localPopularMax)
		if err != nil {
			return nil, err
		}
		if err := l.cache.SetexCtx(lctx, localPopularKey, encodeLocalIds(ids), localCacheTime); err != nil {
			logx.WithContext(lctx).Errorf("inline: local popular cache: %v", err)
		}
		return ids, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-got:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]int64), nil
	}
}

// parseLocalOffset reads an offset Search gave: "" for the first page, or
// a positive index in its decimal form.
func parseLocalOffset(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > localPopularMax || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// localOffset is the offset of the page starting at index at of a list of
// n GIFs: "" at the end, which ends the list for every client (tdesktop
// gifs_list_widget.cpp:944, Android EmojiView.java).
func localOffset(at, n int) string {
	if at >= n {
		return ""
	}
	return strconv.Itoa(at)
}

func encodeLocalIds(ids []int64) string {
	var b strings.Builder
	b.WriteString(localPopularTag)
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(id, 10))
	}
	return b.String()
}

// decodeLocalIds reads a value of encodeLocalIds; ok is false for a key
// that is not there or a value of another form, which is counted again.
func decodeLocalIds(v string) ([]int64, bool) {
	rest, ok := strings.CutPrefix(v, localPopularTag)
	if !ok {
		return nil, false
	}
	if rest == "" {
		return []int64{}, true
	}
	parts := strings.Split(rest, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// isGif tells whether clients show the document as a GIF: image/gif, or
// video/mp4 with documentAttributeAnimated, as app/bff/gifs decides it for
// the saved GIFs (Android MessageObject.isGifDocument and
// isNewGifDocument).
func isGif(doc *mtproto.Document) bool {
	if doc.GetPredicateName() != mtproto.Predicate_document || doc.GetId() == 0 {
		return false
	}
	switch doc.GetMimeType() {
	case "image/gif":
		return true
	case "video/mp4":
		for _, a := range doc.GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeAnimated {
				return true
			}
		}
	}
	return false
}

// GifCounts counts the popular GIFs in MySQL: the sends from @gif's
// results (inline_gif_sends, migrate-20261119-inline-bots.sql) and the GIFs
// saved by enough people (user_saved_gifs, migrate-20261030-saved-gifs.sql).
type GifCounts struct {
	db *sqlx.DB
}

// NewGifCounts returns the counts on db.
func NewGifCounts(db *sqlx.DB) *GifCounts {
	return &GifCounts{db: db}
}

// PopularGifs scores a GIF by its sends from @gif's results in the last
// localSendsDays UTC days plus the people who saved it, when they are at
// least localSaversMin, and returns the ids of the limit best, the newest
// document first among equal scores. It then deletes sends older than the
// days counted, a batch at a time: a failure of that is logged, the list
// stands.
func (g *GifCounts) PopularGifs(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	since := now.UTC().AddDate(0, 0, -(localSendsDays - 1)).Format(time.DateOnly)
	var ids []int64
	err := g.db.QueryRowsPartial(ctx, &ids,
		"SELECT document_id FROM ("+
			"SELECT document_id, SUM(sends) AS n FROM inline_gif_sends WHERE day >= ? GROUP BY document_id "+
			"UNION ALL "+
			"SELECT document_id, COUNT(*) AS n FROM user_saved_gifs GROUP BY document_id HAVING COUNT(*) >= ?"+
			") AS t GROUP BY document_id ORDER BY SUM(n) DESC, document_id DESC LIMIT ?",
		since, localSaversMin, limit)
	if err != nil {
		return nil, fmt.Errorf("inline: popular gifs: %w", err)
	}
	if _, err := g.db.Exec(ctx, "DELETE FROM inline_gif_sends WHERE day < ? LIMIT ?", since, localSendsDeleteBatch); err != nil {
		logx.WithContext(ctx).Errorf("inline: old gif sends: %v", err)
	}
	if ids == nil {
		ids = []int64{}
	}
	return ids, nil
}
