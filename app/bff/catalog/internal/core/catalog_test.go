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
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/demand"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
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
	c := New(context.Background(), &svc.ServiceContext{Catalog: m, Media: fm, Installed: &memInstalled{}, Appearance: newMemAppearance()})
	c.MD = &metadata.RpcMetadata{UserId: 1}
	return c
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
	c := New(context.Background(), &svc.ServiceContext{Catalog: &memCatalog{}, Media: &fakeMedia{}, Demand: d, Installed: &memInstalled{}, Appearance: newMemAppearance()})
	c.MD = &metadata.RpcMetadata{UserId: 1}
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

// memInstalled is the installed sets of a test, with the updates it would
// push.
type memInstalled struct {
	rows    map[int64][]installed.Entry // by user, installed first in order
	updates []*mtproto.Update
	toggled []installed.Ref
	action  installed.Action

	// The sets users make and the trending sets seen (task 084).
	changed, dropped         []installed.Ref
	read                     map[int64][]int64
	readStickers, readEmojis []int64
}

func (m *memInstalled) Installed(_ context.Context, user int64, kind installed.Kind) ([]installed.Entry, error) {
	var out []installed.Entry
	for _, e := range m.rows[user] {
		if e.Kind == kind && e.ArchivedAt == 0 {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memInstalled) Archived(_ context.Context, user int64, kind installed.Kind) ([]installed.Entry, error) {
	var out []installed.Entry
	for _, e := range m.rows[user] {
		if e.Kind == kind && e.ArchivedAt > 0 {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memInstalled) Entry(_ context.Context, user, set int64) (*installed.Entry, error) {
	for _, e := range m.rows[user] {
		if e.SetID == set {
			return &e, nil
		}
	}
	return nil, nil
}

func (m *memInstalled) Install(_ context.Context, user int64, kind installed.Kind, set int64, archived bool, newSet func(int64) *mtproto.Update) error {
	if m.rows == nil {
		m.rows = map[int64][]installed.Entry{}
	}
	m.rows[user] = append([]installed.Entry{{SetID: set, Kind: kind, InstalledAt: 1700000000}}, m.rows[user]...)
	if !archived {
		m.updates = append(m.updates, newSet(1700000000))
	}
	return nil
}

func (m *memInstalled) Toggle(_ context.Context, _ int64, action installed.Action, sets []installed.Ref) error {
	m.action, m.toggled = action, sets
	return nil
}

func (m *memInstalled) Reorder(_ context.Context, _ int64, _ installed.Kind, order []int64) ([]int64, error) {
	return order, nil
}

func setOf(t *testing.T, id int64, name string, hash int32, emojis bool) *catalog.Set {
	t.Helper()
	ms := mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set:       mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: id, AccessHash: id + 1, ShortName: name, Hash: hash, Emojis: emojis}).To_StickerSet(),
		Documents: []*mtproto.Document{mtproto.MakeTLDocument(&mtproto.Document{Id: id * 10}).To_Document()},
	}).To_Messages_StickerSet()
	b, err := proto.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	return &catalog.Set{ID: id, AccessHash: id + 1, ShortName: name, Hash: hash, Data: b}
}

// A set installed goes to the account's list with its installed_date; the
// other devices get it whole; the list's hash is the one clients count.
func TestInstallAndList(t *testing.T) {
	ins := &memInstalled{}
	cat := &memCatalog{sets: []*catalog.Set{setOf(t, 10, "Cats", 7, false), setOf(t, 20, "Dogs", -3, false), setOf(t, 30, "Emo", 5, true), setOf(t, 40, "Owls", 1, false)}}
	c := New(context.Background(), &svc.ServiceContext{Catalog: cat, Media: &fakeMedia{}, Installed: ins, Appearance: newMemAppearance()})
	c.MD = &metadata.RpcMetadata{UserId: 42, PermAuthKeyId: 9}
	for _, name := range []string{"Cats", "Dogs", "Emo"} {
		r, err := c.MessagesInstallStickerSet(&mtproto.TLMessagesInstallStickerSet{
			Stickerset: mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: name}).To_InputStickerSet(),
			Archived:   mtproto.BoolFalse,
		})
		if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_stickerSetInstallResultSuccess {
			t.Fatalf("install %s: %v, %v", name, r, err)
		}
	}
	u := ins.updates[0].GetStickerset_MESSAGES_STICKERSET()
	if ins.updates[0].GetPredicateName() != mtproto.Predicate_updateNewStickerSet || u.GetSet().GetInstalledDate().GetValue() != 1700000000 || len(u.GetDocuments()) != 1 {
		t.Fatalf("update %v", ins.updates[0])
	}
	all, err := c.MessagesGetAllStickers(&mtproto.TLMessagesGetAllStickers{})
	if err != nil || len(all.GetSets()) != 2 || all.GetSets()[0].GetShortName() != "Dogs" || all.GetSets()[0].GetInstalledDate() == nil {
		t.Fatalf("all stickers %v, %v", all, err)
	}
	want := uint64(0)
	for _, h := range []int64{-3, 7} {
		want ^= want >> 21
		want ^= want << 35
		want ^= want >> 4
		want += uint64(h)
	}
	if all.GetHash() != int64(want) {
		t.Fatalf("hash %d, want %d", all.GetHash(), int64(want))
	}
	if nm, _ := c.MessagesGetAllStickers(&mtproto.TLMessagesGetAllStickers{Hash: all.GetHash()}); nm.GetPredicateName() != mtproto.Predicate_messages_allStickersNotModified {
		t.Fatalf("with its hash: %v", nm)
	}
	if emo, _ := c.MessagesGetEmojiStickers(&mtproto.TLMessagesGetEmojiStickers{}); len(emo.GetSets()) != 1 || emo.GetSets()[0].GetShortName() != "Emo" {
		t.Fatalf("emoji sets %v", emo)
	}

	// A set by itself says whether the account installed it.
	getSet := func(name string) *mtproto.StickerSet {
		t.Helper()
		ms, err := c.MessagesGetStickerSet(&mtproto.TLMessagesGetStickerSet{
			Stickerset: mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: name}).To_InputStickerSet(),
		})
		if err != nil {
			t.Fatalf("getStickerSet %s: %v", name, err)
		}
		return ms.GetSet()
	}
	if s := getSet("Dogs"); s.GetInstalledDate().GetValue() != 1700000000 || s.GetArchived() {
		t.Fatalf("an installed set by itself: %v", s)
	}
	if s := getSet("Owls"); s.GetInstalledDate() != nil || s.GetArchived() {
		t.Fatalf("a set not installed by itself: %v", s)
	}

	// Archived, a page at a time; emoji sets with all their documents.
	ins.rows[42] = append(ins.rows[42], installed.Entry{SetID: 10, Kind: installed.Stickers, InstalledAt: 1, ArchivedAt: 5}, installed.Entry{SetID: 20, Kind: installed.Stickers, InstalledAt: 1, ArchivedAt: 4})
	arch, err := c.MessagesGetArchivedStickers(&mtproto.TLMessagesGetArchivedStickers{Limit: 1})
	if err != nil || arch.GetCount() != 2 || len(arch.GetSets()) != 1 || !arch.GetSets()[0].GetSet().GetArchived() || arch.GetSets()[0].GetCover().GetId() != 100 {
		t.Fatalf("archived %v, %v", arch, err)
	}
	if next, _ := c.MessagesGetArchivedStickers(&mtproto.TLMessagesGetArchivedStickers{OffsetId: 10, Limit: 5}); len(next.GetSets()) != 1 || next.GetSets()[0].GetSet().GetId() != 20 {
		t.Fatalf("the next page %v", next)
	}
	if n, _ := c.MessagesGetArchivedStickers(&mtproto.TLMessagesGetArchivedStickers{}); n.GetCount() != 2 || len(n.GetSets()) != 0 {
		t.Fatalf("limit 0, the count alone: %v", n)
	}
	ins.rows[42] = []installed.Entry{{SetID: 40, Kind: installed.Stickers, InstalledAt: 3, ArchivedAt: 6}}
	if s := getSet("Owls"); s.GetInstalledDate().GetValue() != 3 || !s.GetArchived() {
		t.Fatalf("an archived set by itself: %v", s)
	}

	// Exactly one action; the sets' kinds go with them.
	if _, err := c.MessagesToggleStickerSets(&mtproto.TLMessagesToggleStickerSets{Archive: true, Uninstall: true}); !errors.Is(err, mtproto.ErrInputRequestInvalid) {
		t.Fatalf("two actions: %v", err)
	}
	if _, err := c.MessagesToggleStickerSets(&mtproto.TLMessagesToggleStickerSets{Archive: true, Stickersets: []*mtproto.InputStickerSet{
		mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 30, AccessHash: 31}).To_InputStickerSet(),
	}}); err != nil || ins.action != installed.Archive || len(ins.toggled) != 1 || ins.toggled[0].Kind != installed.Emojis {
		t.Fatalf("archive: %v, %v %v", err, ins.action, ins.toggled)
	}
}
