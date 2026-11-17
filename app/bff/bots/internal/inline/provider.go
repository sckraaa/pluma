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

// Package inline holds the providers of the built-in inline bots
// (pkg/inlinebots, task 099, ADR 0062): what answers a query to @gif or
// @pic in place of a bot process. A provider finds media; the bots service
// imports what is not on the server yet (pkg/inlinemedia) and makes the
// results.
package inline

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
)

// Query is an inline query to a built-in bot.
type Query struct {
	// Text is the query, at most 256 characters; empty for the popular
	// media (the GIF tab of the emoji panel asks with an empty query,
	// Android EmojiView.java:8939).
	Text string
	// Offset is the next_offset of the page before, empty for the first:
	// what the provider put there itself, at most 64 bytes.
	Offset string
	// Lang is the language of the user's app (the session's lang_code),
	// empty when unknown.
	Lang string
}

// Item is a medium of a page: one already on the server (Document or
// Photo), or one found elsewhere to import (Source).
type Item struct {
	Document *mtproto.Document
	Photo    *mtproto.Photo
	Source   *inlinemedia.Source
	// Next is the offset of the page that goes on after this item: when
	// the answer has no time left for the items after it, next_offset is
	// this. At most 64 bytes.
	Next string
}

// Page is a provider's answer.
type Page struct {
	Items []Item
	// Next is the offset of the next page, empty when there is none.
	Next string
	// CacheTime is how long, in seconds, the page may be served again for
	// the same query to everyone (messages.botResults cache_time).
	CacheTime int32
}

// Provider answers the queries of one built-in bot. An error is a
// provider that cannot answer now: the user gets an empty list that is not
// cached. Search must return before ctx ends. Its errors are logged, so
// they must not carry the query text (an *url.Error of net/http has its
// address dropped before the log).
type Provider interface {
	Search(ctx context.Context, q Query) (Page, error)
}

// Config is InlineBots of the bots configuration: the providers of the
// built-in bots and the budget of their imports (docs/ops/inline-bots.md).
type Config struct {
	// SearxngURL is the SearXNG instance @pic searches pictures with,
	// ${SEARXNG_URL} (http://searxng:8080); empty: @pic has no provider
	// and answers with empty lists.
	SearxngURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ImportDailyBytes bounds the bytes imported into dfs in a UTC day,
	// ${INLINE_IMPORT_DAILY_BYTES}; empty is 1 GiB
	// (inlinemedia.ParseDailyBytes).
	ImportDailyBytes string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
