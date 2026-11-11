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
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/catalog/search"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerfile"
)

// The search of sets and stickers (task 084, ADR 0053) over every stored
// set: the catalog's, those fetched on demand and those users made
// (pkg/catalog/search).

// The most a search answers at once: this server's bounds. The clients
// ask for 50 to 128 stickers a page (Telegram Desktop
// stickers_list_widget.cpp:683, Android MediaDataController.java:10299,
// iOS SearchStickers.swift:309).
const (
	maxFoundStickers = 200
	maxFoundEmoji    = 200
	maxQuery         = 64 // characters of a query that are read
	maxQueryEmoji    = 100
	coversOfFoundSet = 5
)

// searchReady brings the search index up to the stored sets and returns
// the account's own sets of kind, whose results go first.
func (c *CatalogCore) searchReady(method string, kind installed.Kind) ([]int64, error) {
	if err := c.svcCtx.Search.Refresh(c.ctx); err != nil {
		c.Logger.Errorf("%s - the search index: %v", method, err)
		return nil, err
	}
	rows, err := c.svcCtx.Installed.Installed(c.ctx, c.MD.UserId, kind)
	if err != nil {
		c.Logger.Errorf("%s - the account's sets: %v", method, err)
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.SetID
	}
	return ids, nil
}

func cutQuery(q string) string {
	q = strings.TrimSpace(q)
	if r := []rune(q); len(r) > maxQuery {
		q = string(r[:maxQuery])
	}
	return q
}

// searchSets answers messages.searchStickerSets and
// messages.searchEmojiStickerSets: the sets of kind whose title has words
// beginning with the words of the query, or whose short name has the
// query; the account's own first, then the trending ones. A set of custom
// emoji comes with all its emoji, a set of stickers with its first
// stickers (stickerSetMultiCovered), as clients draw a found set by them
// (Android EmojiView.java:6740, Telegram Desktop data_stickers.cpp:1637).
func (c *CatalogCore) searchSets(method string, kind installed.Kind, q string, excludeFeatured bool, hash int64) (*mtproto.Messages_FoundStickerSets, error) {
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	first, err := c.searchReady(method, kind)
	if err != nil {
		return nil, err
	}
	found := c.svcCtx.Search.Sets(kind, cutQuery(q), first, excludeFeatured)
	var h uint64
	for _, f := range found {
		for _, v := range []uint64{uint64(f.ID), uint64(int64(f.Hash))} {
			h ^= h >> 21
			h ^= h << 35
			h ^= h >> 4
			h += v
		}
	}
	if notModified(hash, int64(h)) {
		return mtproto.MakeTLMessagesFoundStickerSetsNotModified(nil).To_Messages_FoundStickerSets(), nil
	}
	sets := make([]*mtproto.StickerSetCovered, 0, len(found))
	for _, f := range found {
		set, err := c.svcCtx.Catalog.SetByID(c.ctx, f.ID)
		if err != nil {
			c.Logger.Errorf("%s - the catalog: %v", method, err)
			return nil, err
		}
		if set == nil {
			continue // gone since the index read it
		}
		ms, err := c.viewOf(method, set)
		if err != nil {
			return nil, err
		}
		if kind == installed.Emojis || len(ms.GetDocuments()) == 0 {
			sets = append(sets, covered(ms))
			continue
		}
		sets = append(sets, mtproto.MakeTLStickerSetMultiCovered(&mtproto.StickerSetCovered{
			Set: ms.GetSet(), Covers: ms.GetDocuments()[:min(coversOfFoundSet, len(ms.GetDocuments()))],
		}).To_StickerSetCovered())
	}
	return mtproto.MakeTLMessagesFoundStickerSets(&mtproto.Messages_FoundStickerSets{Hash: int64(h), Sets: sets}).To_Messages_FoundStickerSets(), nil
}

// queryEmoji reads the emoji of a search: those the client names
// (emoticon, one after another), and any in the text. Anything else in
// emoticon is skipped.
func queryEmoji(texts ...string) []string {
	var out []string
	for _, text := range texts {
		for text != "" && len(out) < maxQueryEmoji {
			n := stickerfile.EmojiPrefix(text)
			if n > 0 {
				if e := search.Normalize(text[:n]); e != "" {
					out = append(out, e)
				}
			} else {
				_, n = utf8.DecodeRuneInString(text)
			}
			text = text[n:]
		}
	}
	return out
}

// foundStickers finds the stickers (or custom emoji) of a search by q and
// emoticon: those that stand for an emoji the client names or the words of
// q stand for in the languages of langs (search.Index.EmojiFor), and those
// with a keyword a word of q begins.
func (c *CatalogCore) foundStickers(method string, emojis bool, q, emoticon string, langs []string) ([]int64, error) {
	kind := installed.Stickers
	if emojis {
		kind = installed.Emojis
	}
	first, err := c.searchReady(method, kind)
	if err != nil {
		return nil, err
	}
	q = strings.ToLower(cutQuery(q))
	words := search.Words(q)
	emoji := queryEmoji(emoticon, q)
	if len(words) > 0 {
		if len(langs) > 4 {
			langs = langs[:4]
		}
		more, err := c.svcCtx.Search.EmojiFor(c.ctx, words, langs)
		if err != nil {
			c.Logger.Errorf("%s - the emoji keywords: %v", method, err)
			return nil, err
		}
		emoji = append(emoji, more...)
	}
	return c.svcCtx.Search.Stickers(emojis, emoji, words, first), nil
}

// idsHash is the hash of ids in their order
// (core.telegram.org/api/offsets#hash-generation).
func idsHash(ids []int64) int64 {
	var h uint64
	for _, id := range ids {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(id)
	}
	return int64(h)
}

// documentsInOrder reads the documents of ids, in their order; one that is
// gone is left out.
func (c *CatalogCore) documentsInOrder(method string, ids []int64) ([]*mtproto.Document, error) {
	out := make([]*mtproto.Document, 0, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	docs, err := c.svcCtx.Media.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{IdList: ids})
	if err != nil {
		c.Logger.Errorf("%s - media: %v", method, err)
		return nil, err
	}
	byID := make(map[int64]*mtproto.Document, len(docs.GetDatas()))
	for _, d := range docs.GetDatas() {
		if d.GetPredicateName() == mtproto.Predicate_document {
			byID[d.GetId()] = d
		}
	}
	for _, id := range ids {
		if d := byID[id]; d != nil {
			out = append(out, d)
		}
	}
	return out, nil
}
