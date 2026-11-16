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

package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/inline"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/metric"
)

// The answers of the built-in inline bots (task 099, ADR 0062).
const (
	// builtinWait bounds the provider's search and the imports of a page:
	// what is not ready by then is left for the next page.
	builtinWait = 7 * time.Second
	// builtinCacheKind is the chat kind of the cache key of a built-in
	// bot's answer: it is the same in every chat and for every user.
	builtinCacheKind = "builtin"
)

// builtinRate bounds the queries of one user to the built-in bots that the
// cache does not answer: typing sends a query a keystroke, and each may
// fetch from the web.
var builtinRate = logincode.Rule{Name: "inline_builtin_rate", Limit: 60, Window: time.Minute}

var (
	metricBuiltinRequests = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "inline",
		Name:      "builtin_requests_total",
		Help:      "Queries to the built-in inline bots the cache did not answer, by bot and result: ok, empty, error (provider failed or no time left), limited.",
		Labels:    []string{"bot", "result"},
	})
	metricBuiltinLatency = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "inline",
		Name:      "builtin_latency_seconds",
		Help:      "Time to answer a query to a built-in inline bot the cache did not answer, in seconds.",
		Labels:    []string{"bot"},
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 7, 10},
	})
)

// builtinResults answers a query to a bot of the registry in place of a bot
// process: from the shared cache, or from the bot's provider, with the
// media it finds imported into dfs. Clients take the GIFs of @gif as
// documents (Web K appGifsManager.ts:81 keeps only
// botInlineMediaResult.document, Android EmojiView.java:9244, iOS
// GifContext.swift:131-134) and the pictures of @pic as photos (Android's
// picture search sends result.photo as a photo of its own,
// SendMessagesHelper.java:11188-11189, and fetches a URL result from the
// device, :11185-11200), so nothing in the answer points off the server.
func (c *BotsCore) builtinResults(bot inlinebots.Bot, in *mtproto.TLMessagesGetInlineBotResults) (*mtproto.Messages_BotResults, error) {
	if !c.svcCtx.InlineBots.Usable(bot.Id) {
		return nil, mtproto.ErrBotInlineDisabled
	}
	cacheKey := dao.InlineCacheKey(bot.Id, 0, builtinCacheKind, nil, in.Query, in.Offset)
	q, err := c.svcCtx.CachedInlineQuery(c.ctx, cacheKey)
	if err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - @%s cache: %v", bot.Username, err)
		return nil, mtproto.ErrInternalServerError
	}
	if q != 0 {
		a, err := c.svcCtx.InlineAnswer(c.ctx, q)
		if err != nil {
			c.Logger.Errorf("messages.getInlineBotResults - @%s cache: %v", bot.Username, err)
			return nil, mtproto.ErrInternalServerError
		}
		if a != nil {
			return c.inlineResults(q, a, bot.Id)
		}
	}

	if err := c.svcCtx.Limiter.Hit(c.ctx, builtinRate, strconv.FormatInt(c.MD.UserId, 10)); err != nil {
		if errors.Is(err, mtproto.ErrInternalServerError) {
			return nil, err
		}
		metricBuiltinRequests.Inc(bot.Username, "limited")
		return nil, err
	}
	if !c.svcCtx.EnterWait(c.MD.UserId) {
		return nil, mtproto.ErrBotResponseTimeout
	}
	defer c.svcCtx.LeaveWait(c.MD.UserId)

	start := time.Now()
	a, result := c.builtinAnswer(bot, in)
	metricBuiltinRequests.Inc(bot.Username, result)
	metricBuiltinLatency.ObserveFloat(time.Since(start).Seconds(), bot.Username)

	queryId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.PutInlineAnswer(c.ctx, queryId, a); err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - @%s answer: %v", bot.Username, err)
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.CacheInline(c.ctx, cacheKey, queryId, a.CacheTime); err != nil {
		// The cache spares the provider a repeat of the query; the answer
		// stands.
		c.Logger.Errorf("messages.getInlineBotResults - @%s cache: %v", bot.Username, err)
	}
	return c.inlineResults(queryId, a, bot.Id)
}

// builtinAnswer asks the bot's provider and makes the answer, with the
// result for the metrics. A provider that fails, or a page with nothing
// ready in time or nothing imported, is an empty answer that is not
// cached: clients show
// "nothing found", where an error of the method would only be repeated by
// them (tgnet repeats a 500). The query text is never logged.
func (c *BotsCore) builtinAnswer(bot inlinebots.Bot, in *mtproto.TLMessagesGetInlineBotResults) (*dao.InlineAnswer, string) {
	empty := &dao.InlineAnswer{Results: []*mtproto.BotInlineResult{}, Gallery: true, Bot: bot.Id}
	provider := c.svcCtx.InlineProviders[bot.Kind]
	if provider == nil {
		return empty, "empty"
	}
	ctx, cancel := context.WithTimeout(c.ctx, builtinWait)
	defer cancel()
	page, err := provider.Search(ctx, inline.Query{Text: in.Query, Offset: in.Offset, Lang: c.MD.LangCode})
	if err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - @%s provider: %v", bot.Username, redactURL(err))
		return empty, "error"
	}
	if len(page.Items) == 0 {
		a := *empty
		a.CacheTime = page.CacheTime
		return &a, "empty"
	}
	results, next, complete, err := c.builtinItems(ctx, bot, page)
	if err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - @%s media: %v", bot.Username, err)
		return empty, "error"
	}
	if len(results) == 0 && !complete {
		// Out of time before anything was ready.
		return empty, "error"
	}
	if len(next) > maxNextOffset {
		c.Logger.Errorf("messages.getInlineBotResults - @%s: a next offset of %d bytes", bot.Username, len(next))
		next = ""
	}
	a := &dao.InlineAnswer{
		Results:    results,
		NextOffset: next,
		// A grid of media, as Telegram's @gif and @pic answer (unverified);
		// Android lays out a gallery answer as a grid
		// (MentionsAdapter.java:878).
		Gallery: true,
		Bot:     bot.Id,
	}
	if len(results) == 0 {
		// The provider found media and none could be had: the day's budget
		// is spent, or their hosts failed. Not cached, as for a provider
		// that fails: the next query may have them.
		return a, "empty"
	}
	if complete {
		// A page cut short is not cached: the next query has more of it
		// imported.
		a.CacheTime = page.CacheTime
	}
	return a, "ok"
}

// builtinItems makes the results of a page: the media already on the
// server as they are, the others imported (inlinemedia.Importer.Ensure)
// until ctx ends. complete is false when ctx ended before every item was
// tried; next is then the offset after the last item tried, so the next
// page goes on from there. An item that could not be had, or is not of the
// bot's kind, is left out.
func (c *BotsCore) builtinItems(ctx context.Context, bot inlinebots.Bot, page inline.Page) ([]*mtproto.BotInlineResult, string, bool, error) {
	items := page.Items
	if len(items) > maxInlineResults {
		items = items[:maxInlineResults]
	}
	var (
		sources []inlinemedia.Source
		at      []int // the index in items of each source
	)
	for i, it := range items {
		if it.Source != nil && it.Document == nil && it.Photo == nil {
			sources = append(sources, *it.Source)
			at = append(at, i)
		}
	}
	imported := make(map[int]*inlinemedia.Media, len(sources))
	tried := len(items)
	if len(sources) > 0 {
		got, err := c.svcCtx.InlineMedia.Ensure(ctx, bot.Id, sources)
		if err != nil {
			return nil, "", false, err
		}
		for j, m := range got.Media {
			if m != nil {
				imported[at[j]] = m
			}
		}
		if got.Polled < len(sources) {
			tried = at[got.Polled]
		}
	}

	results := make([]*mtproto.BotInlineResult, 0, tried)
	seen := make(map[string]bool, tried)
	for i := 0; i < tried; i++ {
		it := items[i]
		var (
			id       string
			document = it.Document
			photo    = it.Photo
		)
		if m := imported[i]; m != nil {
			id = it.Source.Key().String()
			document, photo = m.Document, m.Photo
		} else if it.Source != nil && document == nil && photo == nil {
			continue
		}
		r := builtinResult(bot, id, document, photo)
		if r == nil || seen[r.GetId()] {
			continue
		}
		seen[r.GetId()] = true
		results = append(results, r)
	}

	switch {
	case tried == len(items) && len(items) == len(page.Items):
		return results, page.Next, true, nil
	case tried == len(items):
		// More items than a page takes: the rest are not offered.
		return results, items[tried-1].Next, true, nil
	case tried == 0:
		return results, "", false, nil
	}
	return results, items[tried-1].Next, false, nil
}

// builtinResult is the result of a medium of the bot: for @gif a document
// sent as a GIF (type "gif"), for @pic a photo (type "photo"); both send
// the medium itself (botInlineMessageMediaAuto), which
// messages.sendInlineBotResult turns into messageMediaDocument and
// messageMediaPhoto (outOfResult). nil for a medium not of the bot's kind.
// id is the key of an imported medium in hex; a medium that was on the
// server before has its own (resultIdOf).
func builtinResult(bot inlinebots.Bot, id string, document *mtproto.Document, photo *mtproto.Photo) *mtproto.BotInlineResult {
	send := mtproto.MakeTLBotInlineMessageMediaAuto(&mtproto.BotInlineMessage{}).To_BotInlineMessage()
	switch bot.Kind {
	case inlinebots.KindGif:
		if document.GetPredicateName() != mtproto.Predicate_document || document.GetId() == 0 {
			return nil
		}
		if id == "" {
			id = resultIdOf("document", document.GetId())
		}
		return mtproto.MakeTLBotInlineMediaResult(&mtproto.BotInlineResult{
			Id: id, Type: "gif", Document: document, SendMessage: send,
		}).To_BotInlineResult()
	case inlinebots.KindPhoto:
		if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetId() == 0 {
			return nil
		}
		if id == "" {
			id = resultIdOf("photo", photo.GetId())
		}
		return mtproto.MakeTLBotInlineMediaResult(&mtproto.BotInlineResult{
			Id: id, Type: "photo", Photo: photo, SendMessage: send,
		}).To_BotInlineResult()
	}
	return nil
}

// resultIdOf is the result id of a medium already on the server: the key
// of its kind and id, of the same form as an imported medium's.
func resultIdOf(kind string, id int64) string {
	return inlinemedia.KeyOf(kind, strconv.FormatInt(id, 10)).String()
}

// redactURL drops the address from an error of net/http, which names it:
// a provider's address carries the query text.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
