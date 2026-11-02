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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/demand"
	"google.golang.org/protobuf/proto"
)

// memCatalog is the catalog of a test.
type memCatalog struct {
	answers map[[2]string]*catalog.Answer
	sets    []*catalog.Set
	docs    map[int64]bool
}

func (m *memCatalog) Answer(_ context.Context, method, arg string) (*catalog.Answer, error) {
	return m.answers[[2]string{method, arg}], nil
}

func (m *memCatalog) find(f func(*catalog.Set) bool) *catalog.Set {
	for _, s := range m.sets {
		if f(s) {
			return s
		}
	}
	return nil
}

func (m *memCatalog) SetByID(_ context.Context, id int64) (*catalog.Set, error) {
	return m.find(func(s *catalog.Set) bool { return s.ID == id }), nil
}

func (m *memCatalog) SetByShortName(_ context.Context, name string) (*catalog.Set, error) {
	return m.find(func(s *catalog.Set) bool { return s.ShortName == name }), nil
}

func (m *memCatalog) SetByKind(_ context.Context, kind string) (*catalog.Set, error) {
	return m.find(func(s *catalog.Set) bool { return kind != "" && s.Kind == kind }), nil
}

func (m *memCatalog) CatalogDocuments(_ context.Context, ids []int64) ([]int64, error) {
	out := []int64{}
	for _, id := range ids {
		if m.docs[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// fakeMedia answers MediaGetDocumentList with a document per id.
type fakeMedia struct {
	media_client.MediaClient
	asked []int64
}

func (f *fakeMedia) MediaGetDocumentList(_ context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error) {
	f.asked = in.GetIdList()
	out := &media.Vector_Document{}
	for _, id := range in.GetIdList() {
		out.Datas = append(out.Datas, mtproto.MakeTLDocument(&mtproto.Document{Id: id}).To_Document())
	}
	return out, nil
}

func newCore(t *testing.T, m *memCatalog, fm *fakeMedia) *CatalogCore {
	t.Helper()
	return New(context.Background(), &svc.ServiceContext{Catalog: m, Media: fm})
}

func put(t *testing.T, m *memCatalog, method, arg string, hash int64, msg proto.Message) {
	t.Helper()
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if m.answers == nil {
		m.answers = map[[2]string]*catalog.Answer{}
	}
	m.answers[[2]string{method, arg}] = &catalog.Answer{Hash: hash, Data: b}
}

// Before an import every method of the catalog answers an empty list with
// hash 0, never an error (Android asks again every second after one).
func TestEmptyCatalog(t *testing.T) {
	c := newCore(t, &memCatalog{}, &fakeMedia{})
	r, err := c.MessagesGetAvailableReactions(&mtproto.TLMessagesGetAvailableReactions{Hash: 5})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_availableReactions || len(r.GetReactions()) != 0 || r.GetHash() != 0 {
		t.Fatalf("reactions %v, %v", r, err)
	}
	f, err := c.MessagesGetFeaturedStickers(&mtproto.TLMessagesGetFeaturedStickers{})
	if err != nil || f.GetPredicateName() != mtproto.Predicate_messages_featuredStickers || f.GetSets() == nil {
		t.Fatalf("featured %v, %v", f, err)
	}
	k, err := c.MessagesGetEmojiKeywordsDifference(&mtproto.TLMessagesGetEmojiKeywordsDifference{LangCode: "ru", FromVersion: 4})
	if err != nil || k.GetLangCode() != "ru" || k.GetVersion() != 4 || len(k.GetKeywords()) != 0 {
		t.Fatalf("keywords %v, %v", k, err)
	}
	if _, err := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{
		Stickerset: mtproto.MakeTLInputStickerSetAnimatedEmoji(nil).To_InputStickerSet(),
	}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Fatalf("a set before the import: %v", err)
	}
}

// A stored answer goes whole for hash 0 or another hash, and as
// NotModified for its own.
func TestStoredAnswer(t *testing.T) {
	m := &memCatalog{}
	put(t, m, "messages.getAvailableReactions", "", 77, mtproto.MakeTLMessagesAvailableReactions(&mtproto.Messages_AvailableReactions{
		Hash:      77,
		Reactions: []*mtproto.AvailableReaction{mtproto.MakeTLAvailableReaction(&mtproto.AvailableReaction{Reaction: "❤", Title: "Red Heart"}).To_AvailableReaction()},
	}).To_Messages_AvailableReactions())
	put(t, m, "account.getThemes", "android", 9, mtproto.MakeTLAccountThemes(&mtproto.Account_Themes{Hash: 9, Themes: []*mtproto.Theme{}}).To_Account_Themes())
	c := newCore(t, m, &fakeMedia{})
	for _, hash := range []int32{0, 5} {
		r, err := c.MessagesGetAvailableReactions(&mtproto.TLMessagesGetAvailableReactions{Hash: hash})
		if err != nil || len(r.GetReactions()) != 1 || r.GetReactions()[0].GetTitle() != "Red Heart" || r.GetHash() != 77 {
			t.Fatalf("hash %d: %v, %v", hash, r, err)
		}
	}
	r, err := c.MessagesGetAvailableReactions(&mtproto.TLMessagesGetAvailableReactions{Hash: 77})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_availableReactionsNotModified {
		t.Fatalf("its own hash: %v, %v", r, err)
	}
	// The argument picks the answer: themes of another format are none.
	if th, err := c.AccountGetThemes(&mtproto.TLAccountGetThemes{Format: "ios", Hash: 9}); err != nil || th.GetPredicateName() != mtproto.Predicate_account_themes {
		t.Fatalf("themes of another format: %v, %v", th, err)
	}
	if th, _ := c.AccountGetThemes(&mtproto.TLAccountGetThemes{Format: "android", Hash: 9}); th.GetPredicateName() != mtproto.Predicate_account_themesNotModified {
		t.Fatalf("themes of its format: %v", th)
	}
}

func TestTopReactionsLimit(t *testing.T) {
	m := &memCatalog{}
	var list []*mtproto.Reaction
	for _, e := range []string{"👍", "❤", "🔥"} {
		list = append(list, mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: e}).To_Reaction())
	}
	put(t, m, "messages.getTopReactions", "", 3, mtproto.MakeTLMessagesReactions(&mtproto.Messages_Reactions{Hash: 3, Reactions: list}).To_Messages_Reactions())
	c := newCore(t, m, &fakeMedia{})
	r, err := c.MessagesGetTopReactions(&mtproto.TLMessagesGetTopReactions{Limit: 2, Hash: 3})
	if err != nil || len(r.GetReactions()) != 2 || r.GetHash() != 0 {
		t.Fatalf("a short list: %v, %v", r, err)
	}
	if r, _ := c.MessagesGetTopReactions(&mtproto.TLMessagesGetTopReactions{Limit: 100, Hash: 3}); r.GetPredicateName() != mtproto.Predicate_messages_reactionsNotModified {
		t.Fatalf("the whole list: %v", r)
	}
}

func TestKeywordsDifference(t *testing.T) {
	m := &memCatalog{}
	put(t, m, "messages.getEmojiKeywords", "ru", 0, mtproto.MakeTLEmojiKeywordsDifference(&mtproto.EmojiKeywordsDifference{
		LangCode: "ru", Version: 12,
		Keywords: []*mtproto.EmojiKeyword{mtproto.MakeTLEmojiKeyword(&mtproto.EmojiKeyword{Keyword: "сердце", Emoticons: []string{"❤"}}).To_EmojiKeyword()},
	}).To_EmojiKeywordsDifference())
	c := newCore(t, m, &fakeMedia{})
	if d, _ := c.MessagesGetEmojiKeywordsDifference(&mtproto.TLMessagesGetEmojiKeywordsDifference{LangCode: "ru", FromVersion: 12}); d.GetVersion() != 12 || len(d.GetKeywords()) != 0 {
		t.Fatalf("at the version: %v", d)
	}
	if d, _ := c.MessagesGetEmojiKeywordsDifference(&mtproto.TLMessagesGetEmojiKeywordsDifference{LangCode: "ru", FromVersion: 3}); d.GetFromVersion() != 3 || d.GetVersion() != 12 || len(d.GetKeywords()) != 1 {
		t.Fatalf("from an old version: %v", d)
	}
	langs, _ := c.MessagesGetEmojiKeywordsLanguages(&mtproto.TLMessagesGetEmojiKeywordsLanguages{LangCodes: []string{"en", "ru", "ru-RU"}})
	if len(langs.GetDatas()) != 1 || langs.GetDatas()[0].GetLangCode() != "ru" {
		t.Fatalf("languages %v", langs)
	}
	// A keyboard's regional code finds its language's words.
	if d, _ := c.MessagesGetEmojiKeywords(&mtproto.TLMessagesGetEmojiKeywords{LangCode: "ru-RU"}); d.GetLangCode() != "ru" || len(d.GetKeywords()) != 1 {
		t.Fatalf("ru-RU: %v", d)
	}
	if d, _ := c.MessagesGetEmojiKeywordsDifference(&mtproto.TLMessagesGetEmojiKeywordsDifference{LangCode: "ru_RU", FromVersion: 12}); d.GetLangCode() != "ru" || d.GetVersion() != 12 {
		t.Fatalf("ru_RU at the version: %v", d)
	}
}

func TestStickerSet(t *testing.T) {
	set := mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 10, AccessHash: 11, ShortName: "AnimatedEmojies", Hash: 5}).To_StickerSet(),
	}).To_Messages_StickerSet()
	b, _ := proto.Marshal(set)
	m := &memCatalog{sets: []*catalog.Set{{ID: 10, AccessHash: 11, ShortName: "AnimatedEmojies", Kind: catalog.KindAnimatedEmoji, Hash: 5, Data: b}}}
	c := newCore(t, m, &fakeMedia{})
	for name, in := range map[string]*mtproto.InputStickerSet{
		"by id":   mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 10, AccessHash: 11}).To_InputStickerSet(),
		"by name": mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "AnimatedEmojies"}).To_InputStickerSet(),
		"by kind": mtproto.MakeTLInputStickerSetAnimatedEmoji(nil).To_InputStickerSet(),
	} {
		r, err := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{Stickerset: in})
		if err != nil || r.GetSet().GetShortName() != "AnimatedEmojies" {
			t.Fatalf("%s: %v, %v", name, r, err)
		}
		if r, _ := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{Stickerset: in, Hash: 5}); r.GetPredicateName() != mtproto.Predicate_messages_stickerSetNotModified {
			t.Fatalf("%s with its hash: %v", name, r)
		}
	}
	for name, in := range map[string]*mtproto.InputStickerSet{
		"another access hash": mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 10, AccessHash: 12}).To_InputStickerSet(),
		"another kind":        mtproto.MakeTLInputStickerSetPremiumGifts(nil).To_InputStickerSet(),
		"empty":               mtproto.MakeTLInputStickerSetEmpty(nil).To_InputStickerSet(),
	} {
		if _, err := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{Stickerset: in}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Only documents of the catalog are given by id: a user's document is not.
func TestCustomEmojiDocuments(t *testing.T) {
	fm := &fakeMedia{}
	c := newCore(t, &memCatalog{docs: map[int64]bool{1: true, 3: true}}, fm)
	r, err := c.MessagesGetCustomEmojiDocuments(&mtproto.TLMessagesGetCustomEmojiDocuments{DocumentId: []int64{1, 2, 3}})
	if err != nil || len(r.GetDatas()) != 2 || len(fm.asked) != 2 || fm.asked[0] != 1 || fm.asked[1] != 3 {
		t.Fatalf("documents %v, asked %v, %v", r, fm.asked, err)
	}
	if _, err := c.MessagesGetCustomEmojiDocuments(&mtproto.TLMessagesGetCustomEmojiDocuments{DocumentId: make([]int64, maxCustomEmoji+1)}); !errors.Is(err, mtproto.ErrLimitInvalid) {
		t.Fatalf("too many: %v", err)
	}
}

func TestWallPaper(t *testing.T) {
	m := &memCatalog{}
	w := mtproto.MakeTLWallPaper(&mtproto.WallPaper{Id: 5, AccessHash: 6, Slug: "fqv01SQemVIBAAAApND8LDRUhRU"}).To_WallPaper()
	put(t, m, "account.getWallPaper", "slug:fqv01SQemVIBAAAApND8LDRUhRU", 0, w)
	put(t, m, "account.getWallPaper", "id:5", 0, w)
	c := newCore(t, m, &fakeMedia{})
	for _, in := range []*mtproto.InputWallPaper{
		mtproto.MakeTLInputWallPaperSlug(&mtproto.InputWallPaper{Slug: "fqv01SQemVIBAAAApND8LDRUhRU"}).To_InputWallPaper(),
		mtproto.MakeTLInputWallPaper(&mtproto.InputWallPaper{Id: 5, AccessHash: 6}).To_InputWallPaper(),
	} {
		if r, err := c.AccountGetWallPaper(&mtproto.TLAccountGetWallPaper{Wallpaper: in}); err != nil || r.GetId() != 5 {
			t.Fatalf("%s: %v, %v", in.GetPredicateName(), r, err)
		}
	}
	for _, in := range []*mtproto.InputWallPaper{
		mtproto.MakeTLInputWallPaper(&mtproto.InputWallPaper{Id: 5, AccessHash: 7}).To_InputWallPaper(),
		mtproto.MakeTLInputWallPaperSlug(&mtproto.InputWallPaper{Slug: "none"}).To_InputWallPaper(),
	} {
		if _, err := c.AccountGetWallPaper(&mtproto.TLAccountGetWallPaper{Wallpaper: in}); !errors.Is(err, mtproto.ErrWallpaperInvalid) {
			t.Errorf("%s: %v", in.GetPredicateName(), err)
		}
	}
}

type fakeDemand struct {
	asked []string
	set   *catalog.Set
	err   error
}

func (f *fakeDemand) ByShortName(_ context.Context, name string) (*catalog.Set, error) {
	f.asked = append(f.asked, name)
	return f.set, f.err
}

// A set by name the catalog lacks comes from Telegram; one Telegram has not
// either is STICKERSET_INVALID; a set by id or kind is never fetched.
func TestStickerSetOnDemand(t *testing.T) {
	set := mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 70, ShortName: "Free420", Hash: 3}).To_StickerSet(),
	}).To_Messages_StickerSet()
	b, _ := proto.Marshal(set)
	d := &fakeDemand{set: &catalog.Set{ID: 70, ShortName: "Free420", Hash: 3, Data: b}}
	c := New(context.Background(), &svc.ServiceContext{Catalog: &memCatalog{}, Media: &fakeMedia{}, Demand: d})
	byName := mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "Free420"}).To_InputStickerSet()
	r, err := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{Stickerset: byName})
	if err != nil || r.GetSet().GetId() != 70 || len(d.asked) != 1 {
		t.Fatalf("by name: %v, %v, asked %v", r, err, d.asked)
	}
	d.set, d.err = nil, demand.ErrNoSet
	if _, err := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{Stickerset: byName}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Fatalf("a set Telegram has not: %v", err)
	}
	for _, in := range []*mtproto.InputStickerSet{
		mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 1, AccessHash: 2}).To_InputStickerSet(),
		mtproto.MakeTLInputStickerSetAnimatedEmoji(nil).To_InputStickerSet(),
	} {
		c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{Stickerset: in})
	}
	if len(d.asked) != 2 {
		t.Fatalf("asked %v", d.asked)
	}
}
