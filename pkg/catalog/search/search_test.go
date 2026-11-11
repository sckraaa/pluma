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

package search

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"google.golang.org/protobuf/proto"
)

type memSource struct {
	sets    map[int64]*catalog.Set
	answers map[[2]string]*catalog.Answer
	loads   int
}

func (m *memSource) SetVersions(context.Context) ([]catalog.SetVersion, error) {
	var out []catalog.SetVersion
	for _, s := range m.sets {
		out = append(out, catalog.SetVersion{ID: s.ID, Hash: s.Hash})
	}
	return out, nil
}

func (m *memSource) SetByID(_ context.Context, id int64) (*catalog.Set, error) {
	m.loads++
	return m.sets[id], nil
}

func (m *memSource) Answer(_ context.Context, method, arg string) (*catalog.Answer, error) {
	return m.answers[[2]string{method, arg}], nil
}

func (m *memSource) AnswerHash(_ context.Context, method, arg string) (int64, bool, error) {
	a := m.answers[[2]string{method, arg}]
	if a == nil {
		return 0, false, nil
	}
	return a.Hash, true, nil
}

type item struct {
	id       int64
	emoji    string
	keywords []string
}

func (m *memSource) put(t *testing.T, id int64, hash int32, title, short string, emojis bool, items ...item) {
	t.Helper()
	ms := &mtproto.Messages_StickerSet{Set: &mtproto.StickerSet{Id: id, Title: title, ShortName: short, Emojis: emojis, Hash: hash}}
	for _, it := range items {
		ms.Documents = append(ms.Documents, &mtproto.Document{Id: it.id})
		ms.Packs = append(ms.Packs, &mtproto.StickerPack{Emoticon: it.emoji, Documents: []int64{it.id}})
		if len(it.keywords) > 0 {
			ms.Keywords = append(ms.Keywords, &mtproto.StickerKeyword{DocumentId: it.id, Keyword: it.keywords})
		}
	}
	data, err := proto.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	if m.sets == nil {
		m.sets = map[int64]*catalog.Set{}
	}
	m.sets[id] = &catalog.Set{ID: id, ShortName: short, Hash: hash, Data: data}
}

func (m *memSource) answer(t *testing.T, method, arg string, hash int64, msg proto.Message) {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if m.answers == nil {
		m.answers = map[[2]string]*catalog.Answer{}
	}
	m.answers[[2]string{method, arg}] = &catalog.Answer{Hash: hash, Data: data}
}

func foundIDs(found []FoundSet) []int64 {
	out := []int64{}
	for _, f := range found {
		out = append(out, f.ID)
	}
	return out
}

// The index finds sets by the words of their titles and by their short
// names, the account's own first and the trending ones next, and stickers
// by their emoji, their keywords and the emoji the words of a query stand
// for; it follows the stored sets by their hashes, loading again only
// those that changed.
func TestIndex(t *testing.T) {
	ctx := context.Background()
	src := &memSource{}
	src.put(t, 10, 1, "Funny Cats", "funny_cats", false, item{101, "😺", []string{"kitty"}}, item{102, "😂", nil})
	src.put(t, 20, 1, "Cat Memes", "catmemes", false, item{201, "😺", nil})
	src.put(t, 30, 1, "Dogs", "dogs", false, item{301, "🐶", []string{"Good Boy"}})
	src.put(t, 40, 1, "Cat Emoji", "cat_emoji", true, item{401, "😺", nil}, item{402, "❤️", nil})
	src.answer(t, methodFeatured, "", 7, &mtproto.Messages_FeaturedStickers{
		Sets: []*mtproto.StickerSetCovered{{Set: &mtproto.StickerSet{Id: 20}}},
	})
	src.answer(t, methodKeywords, "en", 3, &mtproto.EmojiKeywordsDifference{Keywords: []*mtproto.EmojiKeyword{
		{PredicateName: mtproto.Predicate_emojiKeyword, Keyword: "dog", Emoticons: []string{"🐶"}},
		{PredicateName: mtproto.Predicate_emojiKeyword, Keyword: "love", Emoticons: []string{"❤"}},
		{PredicateName: mtproto.Predicate_emojiKeywordDeleted, Keyword: "doge", Emoticons: []string{"😂"}},
	}})
	now := time.Unix(1000, 0)
	x := New(src)
	x.now = func() time.Time { return now }
	if err := x.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	if got := foundIDs(x.Sets(installed.Stickers, "cat", nil, false)); !slices.Equal(got, []int64{20, 10}) {
		t.Errorf("sets for cat: %v, want the trending one first", got)
	}
	if got := foundIDs(x.Sets(installed.Stickers, " CAT ", []int64{10, 30}, false)); !slices.Equal(got, []int64{10, 20}) {
		t.Errorf("sets for cat with the account's: %v", got)
	}
	if got := foundIDs(x.Sets(installed.Stickers, "cat", nil, true)); !slices.Equal(got, []int64{10}) {
		t.Errorf("sets for cat without the trending: %v", got)
	}
	if got := foundIDs(x.Sets(installed.Emojis, "cat", nil, false)); !slices.Equal(got, []int64{40}) {
		t.Errorf("emoji sets for cat: %v", got)
	}
	if got := foundIDs(x.Sets(installed.Stickers, "fun ca", nil, false)); !slices.Equal(got, []int64{10}) {
		t.Errorf("sets for two words: %v", got)
	}
	if got := foundIDs(x.Sets(installed.Stickers, "catmem", nil, false)); !slices.Equal(got, []int64{20}) {
		t.Errorf("sets by short name: %v", got)
	}
	for _, q := range []string{"", "  ", "ats", "bird"} {
		if got := x.Sets(installed.Stickers, q, nil, false); len(got) != 0 {
			t.Errorf("sets for %q: %v", q, got)
		}
	}

	if got := x.Stickers(false, []string{"😺"}, nil, []int64{10}); !slices.Equal(got, []int64{101, 201}) {
		t.Errorf("stickers for the emoji: %v", got)
	}
	if got := x.Stickers(false, nil, []string{"kit"}, nil); !slices.Equal(got, []int64{101}) {
		t.Errorf("stickers by a keyword: %v", got)
	}
	if got := x.Stickers(false, nil, []string{"boy"}, nil); !slices.Equal(got, []int64{301}) {
		t.Errorf("stickers by a word of a keyword: %v", got)
	}
	if got := x.Stickers(true, []string{Normalize("❤️"), "😺"}, nil, nil); !slices.Equal(got, []int64{401, 402}) {
		t.Errorf("custom emoji for the emoji: %v", got)
	}
	if got := x.Stickers(false, nil, nil, nil); len(got) != 0 {
		t.Errorf("stickers for nothing: %v", got)
	}
	emoji, err := x.EmojiFor(ctx, []string{"do", "lov"}, []string{"ru", "en-US"})
	if err != nil || !slices.Equal(emoji, []string{"🐶", "❤"}) {
		t.Errorf("the emoji of the words: %q, %v", emoji, err)
	}
	if !x.Featured(20) || x.Featured(10) {
		t.Errorf("the trending sets")
	}

	// Within refreshEvery nothing is read; after it, only what changed.
	loads := src.loads
	src.put(t, 30, 2, "Puppies", "dogs", false, item{301, "🐶", nil})
	delete(src.sets, 20)
	if err := x.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if src.loads != loads || len(x.Sets(installed.Stickers, "pupp", nil, false)) != 0 {
		t.Errorf("the index read the sets again within %v", refreshEvery)
	}
	now = now.Add(refreshEvery + time.Second)
	if err := x.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if src.loads != loads+1 {
		t.Errorf("%d sets loaded again, want the one that changed", src.loads-loads)
	}
	if got := foundIDs(x.Sets(installed.Stickers, "pupp", nil, false)); !slices.Equal(got, []int64{30}) {
		t.Errorf("the changed set: %v", got)
	}
	if got := foundIDs(x.Sets(installed.Stickers, "cat", nil, false)); !slices.Equal(got, []int64{10}) {
		t.Errorf("a removed set is still found: %v", got)
	}
	if got := x.Stickers(false, nil, []string{"boy"}, nil); len(got) != 0 {
		t.Errorf("a keyword the set no longer has: %v", got)
	}
}
