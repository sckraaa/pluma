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

// Package search finds sticker and custom emoji sets by their names, and
// stickers and custom emoji by the emoji they stand for and by their
// keywords (task 084, ADR 0053): messages.searchStickerSets,
// searchEmojiStickerSets, searchStickers and searchCustomEmoji.
//
// The index is in memory, made from the stored sets (the catalog's, those
// fetched on demand and those users made), and follows them: before a
// search, at most once in refreshEvery, it reads the hash of every stored
// set and loads again only the sets whose hash changed. Nothing is kept
// beside the sets, so nothing can disagree with them, and a restart makes
// the index again. For the few thousand sets of a catalog it is a few
// megabytes; the documents themselves are not in it, a search answers ids.
package search

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"google.golang.org/protobuf/proto"
)

// Source is the stored sets and answers (catalog.Store).
type Source interface {
	SetVersions(ctx context.Context) ([]catalog.SetVersion, error)
	SetByID(ctx context.Context, id int64) (*catalog.Set, error)
	Answer(ctx context.Context, method, arg string) (*catalog.Answer, error)
	AnswerHash(ctx context.Context, method, arg string) (int64, bool, error)
}

const refreshEvery = 3 * time.Second

// The stored answers the trending sets and the words of emoji come from.
const (
	methodFeatured      = "messages.getFeaturedStickers"
	methodFeaturedEmoji = "messages.getFeaturedEmojiStickers"
	methodKeywords      = "messages.getEmojiKeywords"
)

type sticker struct {
	id       int64
	emoji    []string // normalized (Normalize)
	keywords []string // lower case
}

type set struct {
	id         int64
	version    catalog.SetVersion
	kind       installed.Kind
	hash       int32
	title      string   // lower case
	shortName  string   // lower case
	titleWords []string // the words of the title and of the short name
	stickers   []sticker
}

type words struct {
	hash    int64
	byWord  map[string][]string // a lower-case word: its emoji, normalized
	ordered []string            // the words, sorted
}

// Index is the search index. The zero value is not usable; see New.
type Index struct {
	source Source
	now    func() time.Time

	refresh sync.Mutex // one refresh at a time
	mu      sync.RWMutex
	at      time.Time
	sets    map[int64]*set
	order   []*set // by id: a stable order of results

	featured     map[int64]bool
	featuredHash [2]int64
	featuredHave [2]bool

	keywords map[string]*words // by language
}

// New returns an empty index over source; it fills on the first search.
func New(source Source) *Index {
	return &Index{source: source, now: time.Now, sets: map[int64]*set{}, featured: map[int64]bool{}, keywords: map[string]*words{}}
}

// Refresh brings the index up to the stored sets if it was not within
// refreshEvery. On an error the index stays as it was.
func (x *Index) Refresh(ctx context.Context) error {
	x.refresh.Lock()
	defer x.refresh.Unlock()
	x.mu.RLock()
	fresh := !x.at.IsZero() && x.now().Sub(x.at) < refreshEvery
	x.mu.RUnlock()
	if fresh {
		return nil
	}
	versions, err := x.source.SetVersions(ctx)
	if err != nil {
		return err
	}
	x.mu.RLock()
	sets := make(map[int64]*set, len(versions))
	var load []catalog.SetVersion
	for _, v := range versions {
		if old := x.sets[v.ID]; old != nil && old.version == v {
			sets[v.ID] = old
		} else {
			load = append(load, v)
		}
	}
	x.mu.RUnlock()
	for _, v := range load {
		stored, err := x.source.SetByID(ctx, v.ID)
		if err != nil {
			return err
		}
		if stored == nil {
			continue // removed meanwhile
		}
		ms := &mtproto.Messages_StickerSet{}
		if err := proto.Unmarshal(stored.Data, ms); err != nil {
			return err
		}
		// The version read first is kept: a set changed since is loaded
		// again by the next refresh.
		sets[v.ID] = entry(v, ms)
	}
	order := make([]*set, 0, len(sets))
	for _, s := range sets {
		order = append(order, s)
	}
	sort.Slice(order, func(i, j int) bool { return order[i].id < order[j].id })

	featured, hashes, have, err := x.loadFeatured(ctx)
	if err != nil {
		return err
	}
	x.mu.Lock()
	x.sets, x.order, x.at = sets, order, x.now()
	if featured != nil {
		x.featured, x.featuredHash, x.featuredHave = featured, hashes, have
	}
	x.mu.Unlock()
	return nil
}

// loadFeatured reads the trending sets again if either list changed; nil
// if neither did.
func (x *Index) loadFeatured(ctx context.Context) (map[int64]bool, [2]int64, [2]bool, error) {
	var (
		hashes [2]int64
		have   [2]bool
	)
	methods := [2]string{methodFeatured, methodFeaturedEmoji}
	for i, m := range methods {
		var err error
		if hashes[i], have[i], err = x.source.AnswerHash(ctx, m, ""); err != nil {
			return nil, hashes, have, err
		}
	}
	x.mu.RLock()
	same := hashes == x.featuredHash && have == x.featuredHave && !x.at.IsZero()
	x.mu.RUnlock()
	if same {
		return nil, hashes, have, nil
	}
	featured := map[int64]bool{}
	for _, m := range methods {
		a, err := x.source.Answer(ctx, m, "")
		if err != nil {
			return nil, hashes, have, err
		}
		if a == nil {
			continue
		}
		answer := &mtproto.Messages_FeaturedStickers{}
		if err := proto.Unmarshal(a.Data, answer); err != nil {
			return nil, hashes, have, err
		}
		for _, c := range answer.GetSets() {
			featured[c.GetSet().GetId()] = true
		}
	}
	return featured, hashes, have, nil
}

func entry(v catalog.SetVersion, ms *mtproto.Messages_StickerSet) *set {
	s := &set{
		id:        v.ID,
		version:   v,
		kind:      installed.KindOf(ms.GetSet()),
		hash:      ms.GetSet().GetHash(),
		title:     strings.ToLower(ms.GetSet().GetTitle()),
		shortName: strings.ToLower(ms.GetSet().GetShortName()),
	}
	s.titleWords = append(append(Words(s.title), Words(s.shortName)...), s.shortName)
	at := make(map[int64]int, len(ms.GetDocuments()))
	s.stickers = make([]sticker, len(ms.GetDocuments()))
	for i, d := range ms.GetDocuments() {
		at[d.GetId()] = i
		s.stickers[i].id = d.GetId()
	}
	named := map[int64]bool{}
	for _, p := range ms.GetPacks() {
		e := Normalize(p.GetEmoticon())
		for _, id := range p.GetDocuments() {
			if i, ok := at[id]; ok && e != "" {
				s.stickers[i].emoji = append(s.stickers[i].emoji, e)
				named[id] = true
			}
		}
	}
	// A sticker its set's packs do not name stands for its own alt.
	for i, d := range ms.GetDocuments() {
		if named[d.GetId()] {
			continue
		}
		for _, a := range d.GetAttributes() {
			switch a.GetPredicateName() {
			case mtproto.Predicate_documentAttributeSticker, mtproto.Predicate_documentAttributeCustomEmoji:
				if e := Normalize(a.GetAlt()); e != "" {
					s.stickers[i].emoji = append(s.stickers[i].emoji, e)
				}
			}
		}
	}
	for _, k := range ms.GetKeywords() {
		if i, ok := at[k.GetDocumentId()]; ok {
			for _, w := range k.GetKeyword() {
				s.stickers[i].keywords = append(s.stickers[i].keywords, Words(strings.ToLower(w))...)
			}
		}
	}
	return s
}

// Normalize is an emoji as the index compares it: without its variation
// selectors, which clients and sets write or leave out freely.
func Normalize(emoji string) string {
	return strings.NewReplacer("️", "", "︎", "").Replace(strings.TrimSpace(emoji))
}

// Words splits lower-case text into its words: runs of letters and digits.
func Words(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// matches tells if every word of the query begins a word of the set's
// title or of its short name (the parts between its underscores, and the
// name whole).
func (s *set) matches(queryWords []string) bool {
	for _, q := range queryWords {
		if !slices.ContainsFunc(s.titleWords, func(w string) bool { return strings.HasPrefix(w, q) }) {
			return false
		}
	}
	return len(queryWords) > 0
}

// FoundSet is a set a search found.
type FoundSet struct {
	ID   int64
	Hash int32
}

// MaxSets is the most sets a search answers: messages.foundStickerSets has
// no pages.
const MaxSets = 50

// Sets finds the sets of kind for query, those every word of which begins
// a word of the set's title or short name: those of first (the account's
// own, in its order) before the rest, the trending ones next; without the
// trending ones if excludeFeatured (a client that has them searches them
// itself).
func (x *Index) Sets(kind installed.Kind, query string, first []int64, excludeFeatured bool) []FoundSet {
	queryWords := Words(strings.ToLower(query))
	if len(queryWords) == 0 {
		return []FoundSet{}
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := []FoundSet{}
	seen := map[int64]bool{}
	take := func(s *set) {
		if s == nil || seen[s.id] || s.kind != kind || len(out) >= MaxSets || !s.matches(queryWords) {
			return
		}
		if excludeFeatured && x.featured[s.id] {
			return
		}
		seen[s.id] = true
		out = append(out, FoundSet{ID: s.id, Hash: s.hash})
	}
	for _, id := range first {
		take(x.sets[id])
	}
	for _, featured := range []bool{true, false} {
		for _, s := range x.order {
			if x.featured[s.id] == featured {
				take(s)
			}
		}
	}
	return out
}

// Stickers finds the stickers (the custom emoji if emojis) that stand for
// one of emoji (normalized) or have a keyword one of words begins, in a
// stable order: those of the sets of first (the account's own, in its
// order), then of the trending sets, then of the rest; each once.
func (x *Index) Stickers(emojis bool, emoji []string, words []string, first []int64) []int64 {
	kind := installed.Stickers
	if emojis {
		kind = installed.Emojis
	}
	want := make(map[string]bool, len(emoji))
	for _, e := range emoji {
		want[e] = true
	}
	out := []int64{}
	if len(want) == 0 && len(words) == 0 {
		return out
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	seenSet := map[int64]bool{}
	seen := map[int64]bool{}
	take := func(s *set) {
		if s == nil || seenSet[s.id] || s.kind != kind {
			return
		}
		seenSet[s.id] = true
		for _, st := range s.stickers {
			if seen[st.id] {
				continue
			}
			hit := slices.ContainsFunc(st.emoji, func(e string) bool { return want[e] })
			if !hit {
				hit = slices.ContainsFunc(words, func(q string) bool {
					return slices.ContainsFunc(st.keywords, func(k string) bool { return strings.HasPrefix(k, q) })
				})
			}
			if hit {
				seen[st.id] = true
				out = append(out, st.id)
			}
		}
	}
	for _, id := range first {
		take(x.sets[id])
	}
	for _, featured := range []bool{true, false} {
		for _, s := range x.order {
			if x.featured[s.id] == featured {
				take(s)
			}
		}
	}
	return out
}

// Featured tells if the set id is a trending one.
func (x *Index) Featured(id int64) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.featured[id]
}

// EmojiFor returns the emoji (normalized) the words of a query stand for
// in the languages langs, by the emoji keywords of the catalog
// (messages.getEmojiKeywords): the emoji of every keyword a word of the
// query begins. Telegram Desktop and Web send the query as typed and leave
// this to the server (stickers_list_widget.cpp:672, appStickersManager.ts
// searchStickersOnServer); Android and iOS also send the emoji they found
// themselves (MediaDataController.java:10283, SearchStickers.swift:309).
func (x *Index) EmojiFor(ctx context.Context, queryWords, langs []string) ([]string, error) {
	var out []string
	for _, lang := range langs {
		w, err := x.words(ctx, lang)
		if err != nil {
			return nil, err
		}
		if w == nil {
			continue
		}
		for _, q := range queryWords {
			for i := sort.SearchStrings(w.ordered, q); i < len(w.ordered) && strings.HasPrefix(w.ordered[i], q); i++ {
				for _, e := range w.byWord[w.ordered[i]] {
					if !slices.Contains(out, e) {
						out = append(out, e)
					}
				}
			}
		}
	}
	return out, nil
}

// words is the emoji keywords of a language, read again when the stored
// answer changed; nil for a language the catalog has none of.
func (x *Index) words(ctx context.Context, lang string) (*words, error) {
	lang = strings.ToLower(strings.ReplaceAll(lang, "_", "-"))
	codes := []string{lang}
	if base, _, ok := strings.Cut(lang, "-"); ok && base != "" {
		codes = append(codes, base)
	}
	for _, code := range codes {
		if code == "" {
			continue
		}
		hash, found, err := x.source.AnswerHash(ctx, methodKeywords, code)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		x.mu.RLock()
		cached := x.keywords[code]
		x.mu.RUnlock()
		if cached != nil && cached.hash == hash {
			return cached, nil
		}
		a, err := x.source.Answer(ctx, methodKeywords, code)
		if err != nil {
			return nil, err
		}
		if a == nil {
			continue
		}
		answer := &mtproto.EmojiKeywordsDifference{}
		if err := proto.Unmarshal(a.Data, answer); err != nil {
			return nil, err
		}
		w := &words{hash: a.Hash, byWord: map[string][]string{}}
		for _, k := range answer.GetKeywords() {
			if k.GetPredicateName() != mtproto.Predicate_emojiKeyword {
				continue
			}
			word := strings.ToLower(k.GetKeyword())
			for _, e := range k.GetEmoticons() {
				if e = Normalize(e); e != "" && !slices.Contains(w.byWord[word], e) {
					w.byWord[word] = append(w.byWord[word], e)
				}
			}
		}
		for word := range w.byWord {
			w.ordered = append(w.ordered, word)
		}
		sort.Strings(w.ordered)
		x.mu.Lock()
		x.keywords[code] = w
		x.mu.Unlock()
		return w, nil
	}
	return nil, nil
}
