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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/catalog/search"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerfile"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
	"google.golang.org/protobuf/proto"
)

// The fakes of the sets users make: the catalog, the accounts' sets and
// the documents of a test, changed as the stores change them.

func (m *memCatalog) SetVersions(context.Context) ([]catalog.SetVersion, error) {
	var out []catalog.SetVersion
	for _, s := range m.sets {
		if s.Kind == "" {
			out = append(out, catalog.SetVersion{ID: s.ID, Hash: s.Hash})
		}
	}
	return out, nil
}

func (m *memCatalog) AnswerHash(_ context.Context, method, arg string) (int64, bool, error) {
	a := m.answers[[2]string{method, arg}]
	if a == nil {
		return 0, false, nil
	}
	return a.Hash, true, nil
}

func (m *memInstalled) Changed(_ context.Context, set int64, kind installed.Kind) error {
	m.changed = append(m.changed, installed.Ref{SetID: set, Kind: kind})
	return nil
}

func (m *memInstalled) Drop(_ context.Context, set int64, kind installed.Kind) error {
	m.dropped = append(m.dropped, installed.Ref{SetID: set, Kind: kind})
	for user, rows := range m.rows {
		m.rows[user] = slices.DeleteFunc(rows, func(e installed.Entry) bool { return e.SetID == set })
	}
	return nil
}

func (m *memInstalled) FeaturedRead(_ context.Context, user int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, id := range m.read[user] {
		out[id] = true
	}
	return out, nil
}

func (m *memInstalled) ReadFeatured(_ context.Context, user, _ int64, stickers, emojis []int64) error {
	if m.read == nil {
		m.read = map[int64][]int64{}
	}
	m.read[user] = append(append(m.read[user], stickers...), emojis...)
	m.readStickers, m.readEmojis = stickers, emojis
	return nil
}

// memUserSets is usersets.Store over the catalog of a test.
type memUserSets struct {
	cat   *memCatalog
	links map[int64][]usersets.Link // by set
	gone  map[int64]int64           // the stickers taken out: their sets
}

func (m *memUserSets) Create(_ context.Context, owner int64, ms *mtproto.Messages_StickerSet, thumb int64, links []usersets.Link, _ int64) (*catalog.Set, error) {
	if taken, _ := m.ShortNameTaken(context.Background(), ms.GetSet().GetShortName()); taken {
		return nil, usersets.ErrShortNameOccupied
	}
	if err := usersets.Seal(ms); err != nil {
		return nil, err
	}
	data, err := proto.Marshal(ms)
	if err != nil {
		return nil, err
	}
	set := &catalog.Set{ID: ms.GetSet().GetId(), AccessHash: ms.GetSet().GetAccessHash(), ShortName: ms.GetSet().GetShortName(),
		Hash: ms.GetSet().GetHash(), Data: data, OwnerID: owner, ThumbDocumentID: thumb}
	m.cat.sets = append(m.cat.sets, set)
	if m.links == nil {
		m.links = map[int64][]usersets.Link{}
	}
	m.links[set.ID] = links
	return set, nil
}

func (m *memUserSets) Update(_ context.Context, id int64, fn func(c *usersets.Change) error) (*catalog.Set, error) {
	set := m.cat.find(func(s *catalog.Set) bool { return s.ID == id && s.OwnerID != 0 })
	if set == nil {
		return nil, usersets.ErrNoSet
	}
	ms := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(set.Data, ms); err != nil {
		return nil, err
	}
	copied := *set
	c := &usersets.Change{Set: &copied, MS: ms, Links: slices.Clone(m.links[id])}
	before := ids(ms)
	if err := fn(c); errors.Is(err, usersets.ErrUnchanged) {
		return set, nil
	} else if err != nil {
		return nil, err
	}
	if err := usersets.Seal(ms); err != nil {
		return nil, err
	}
	data, err := proto.Marshal(ms)
	if err != nil {
		return nil, err
	}
	set.Hash, set.Data, set.ThumbDocumentID = ms.GetSet().GetHash(), data, c.Set.ThumbDocumentID
	// The links follow the stickers of the set, as the store's do by what
	// the change recorded.
	after := ids(ms)
	for _, d := range before {
		if !slices.Contains(after, d) {
			m.links[id] = slices.DeleteFunc(m.links[id], func(l usersets.Link) bool { return l.DocumentID == d })
			if m.gone == nil {
				m.gone = map[int64]int64{}
			}
			m.gone[d] = id
		}
	}
	for _, d := range after {
		if !slices.Contains(before, d) {
			m.links[id] = append(m.links[id], usersets.Link{DocumentID: d, SourceID: sourceOf[d]})
		}
	}
	return set, nil
}

func (m *memUserSets) Delete(_ context.Context, id, owner int64) error {
	n := len(m.cat.sets)
	m.cat.sets = slices.DeleteFunc(m.cat.sets, func(s *catalog.Set) bool { return s.ID == id && s.OwnerID == owner })
	if len(m.cat.sets) == n {
		return usersets.ErrNoSet
	}
	delete(m.links, id)
	return nil
}

func (m *memUserSets) Owned(_ context.Context, owner int64) ([]int64, error) {
	var out []int64
	for i := len(m.cat.sets) - 1; i >= 0; i-- {
		if m.cat.sets[i].OwnerID == owner {
			out = append(out, m.cat.sets[i].ID)
		}
	}
	return out, nil
}

func (m *memUserSets) Links(_ context.Context, id int64) ([]usersets.Link, error) {
	return m.links[id], nil
}

func (m *memUserSets) SetOf(_ context.Context, doc int64) (int64, error) {
	for set, links := range m.links {
		for _, l := range links {
			if l.DocumentID == doc {
				return set, nil
			}
		}
	}
	return 0, nil
}

func (m *memUserSets) ShortNameTaken(_ context.Context, name string) (bool, error) {
	return m.cat.find(func(s *catalog.Set) bool { return s.ShortName == name }) != nil, nil
}

func ids(ms *mtproto.Messages_StickerSet) []int64 {
	out := []int64{}
	for _, d := range ms.GetDocuments() {
		out = append(out, d.GetId())
	}
	return out
}

// sourceOf is the document each sticker stored by memDocuments was made of.
var sourceOf = map[int64]int64{}

// memFiles is the uploaded documents of a test and their files, and stores
// the stickers made of them.
type memFiles struct {
	media *stickerMedia
	files map[int64][]byte
	next  int64
	last  int64 // the document the file read last belongs to
}

func (f *memFiles) upload(id int64, data []byte) *mtproto.InputDocument {
	f.media.docs[id] = mtproto.MakeTLDocument(&mtproto.Document{Id: id, AccessHash: id * 3, Size2_INT64: int64(len(data)), MimeType: "image/webp"}).To_Document()
	f.files[id] = data
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: id, AccessHash: id * 3}).To_InputDocument()
}

func (f *memFiles) DfsDownloadFile(_ context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error) {
	f.last = in.GetLocation().GetId()
	return mtproto.MakeTLUploadFile(&mtproto.Upload_File{Bytes: f.files[in.GetLocation().GetId()]}).To_Upload_File(), nil
}

func (f *memFiles) Import(_ context.Context, doc *mtproto.Document, file []byte, _ map[string][]byte, _ int64) (*mtproto.Document, error) {
	f.next++
	stored := proto.Clone(doc).(*mtproto.Document)
	stored.Id, stored.AccessHash, stored.Size2_INT64 = f.next, f.next*3, int64(len(file))
	f.media.docs[stored.Id] = stored
	f.files[stored.Id] = file
	sourceOf[stored.Id] = f.last
	return stored, nil
}

type ownFixture struct {
	c     *CatalogCore
	cat   *memCatalog
	ins   *memInstalled
	sets  *memUserSets
	files *memFiles
}

func newOwnCore(t *testing.T) *ownFixture {
	t.Helper()
	cat := &memCatalog{}
	md := &stickerMedia{docs: map[int64]*mtproto.Document{}}
	f := &ownFixture{cat: cat, ins: &memInstalled{}, sets: &memUserSets{cat: cat}, files: &memFiles{media: md, files: map[int64][]byte{}, next: 1000}}
	f.c = New(context.Background(), &svc.ServiceContext{
		Catalog: cat, Media: md, Installed: f.ins, Appearance: newMemAppearance(),
		UserSets: f.sets, Documents: f.files, Dfs: f.files, Search: search.New(cat),
	})
	f.c.MD = &metadata.RpcMetadata{UserId: 1}
	return f
}

func (f *ownFixture) as(user int64) *CatalogCore {
	c := New(context.Background(), f.c.svcCtx)
	c.MD = &metadata.RpcMetadata{UserId: user}
	return c
}

func item(doc *mtproto.InputDocument, emoji string) *mtproto.InputStickerSetItem {
	return mtproto.MakeTLInputStickerSetItem(&mtproto.InputStickerSetItem{Document: doc, Emoji: emoji}).To_InputStickerSetItem()
}

func inputOf(d *mtproto.Document) *mtproto.InputDocument {
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: d.GetId(), AccessHash: d.GetAccessHash()}).To_InputDocument()
}

func setRef(ms *mtproto.Messages_StickerSet) *mtproto.InputStickerSet {
	return mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: ms.GetSet().GetId(), AccessHash: ms.GetSet().GetAccessHash()}).To_InputStickerSet()
}

// A set of the account's stickers: made of uploaded files that are checked
// and stored as documents of the set, installed for the account, found by
// its short name by anyone, changed by the account alone, with the
// accounts that have it told of every change.
func TestOwnStickerSet(t *testing.T) {
	f := newOwnCore(t)
	c := f.c
	self := mtproto.MakeTLInputUserSelf(nil).To_InputUser()
	cat1, cat2 := f.files.upload(1, stickerfile.SampleWebp(512, 512)), f.files.upload(2, stickerfile.SampleWebp(512, 400))

	create := func(in *mtproto.TLStickersCreateStickerSet) error {
		_, err := c.StickersCreateStickerSet(in)
		return err
	}
	for name, tc := range map[string]struct {
		in  *mtproto.TLStickersCreateStickerSet
		err error
	}{
		"no stickers":                {&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "T", ShortName: "t"}, mtproto.ErrStickersEmpty},
		"no title":                   {&mtproto.TLStickersCreateStickerSet{UserId: self, Title: " ", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}}, mtproto.ErrPackTitleInvalid},
		"a bad short name":           {&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "T", ShortName: "1t", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}}, mtproto.ErrPackShortNameInvalid},
		"another account":            {&mtproto.TLStickersCreateStickerSet{UserId: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 2}).To_InputUser(), Title: "T", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}}, mtproto.ErrUserIdInvalid},
		"masks and emoji":            {&mtproto.TLStickersCreateStickerSet{UserId: self, Masks: true, Emojis: true, Title: "T", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}}, errPackTypeInvalid},
		"no emoji":                   {&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "T", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "cat")}}, mtproto.ErrStickerEmojiInvalid},
		"a wrong access key":         {&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "T", Stickers: []*mtproto.InputStickerSetItem{item(&mtproto.InputDocument{PredicateName: mtproto.Predicate_inputDocument, Id: 1, AccessHash: 5}, "😺")}}, mtproto.ErrStickerFileInvalid},
		"a file of no sticker":       {&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "T", Stickers: []*mtproto.InputStickerSetItem{item(f.files.upload(9, stickerfile.SampleWebp(300, 300)), "😺")}}, mtproto.ErrStickerPngDimensions},
		"a sticker for an emoji set": {&mtproto.TLStickersCreateStickerSet{UserId: self, Emojis: true, Title: "T", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}}, mtproto.ErrStickerPngDimensions},
	} {
		if err := create(tc.in); !errors.Is(err, tc.err) {
			t.Errorf("stickers.createStickerSet with %s: %v, want %v", name, err, tc.err)
		}
	}
	if len(f.cat.sets) != 0 {
		t.Fatalf("%d sets made by refused requests", len(f.cat.sets))
	}

	// Android and iOS make a set of one sticker with no short name.
	ms, err := c.StickersCreateStickerSet(&mtproto.TLStickersCreateStickerSet{
		UserId: self, Title: "Мои котики", Stickers: []*mtproto.InputStickerSetItem{
			mtproto.MakeTLInputStickerSetItem(&mtproto.InputStickerSetItem{Document: cat1, Emoji: "😺👍", Keywords: mtproto.MakeFlagsString("cat, kitty")}).To_InputStickerSetItem(),
		},
	})
	if err != nil {
		t.Fatalf("stickers.createStickerSet: %v", err)
	}
	set := ms.GetSet()
	if set.GetShortName() != "Moi_kotiki" || set.GetTitle() != "Мои котики" || set.GetCount() != 1 || set.GetHash() == 0 || !set.GetCreator() || set.GetInstalledDate() == nil {
		t.Fatalf("the set made: %v", set)
	}
	first := ms.GetDocuments()[0]
	if first.GetId() == 1 || first.GetMimeType() != "image/webp" {
		t.Fatalf("the sticker is the uploaded document itself, or of another type: %v", first)
	}
	attr := first.GetAttributes()
	if ref := attr[1].GetStickerset(); attr[1].GetPredicateName() != mtproto.Predicate_documentAttributeSticker || attr[1].GetAlt() != "😺" ||
		ref.GetId() != set.GetId() || ref.GetAccessHash() != set.GetAccessHash() || attr[0].GetW() != 512 || attr[0].GetH() != 512 {
		t.Errorf("the attributes of the sticker: %v", attr)
	}
	if len(ms.GetPacks()) != 2 || ms.GetPacks()[1].GetEmoticon() != "👍" || !slices.Equal(ms.GetKeywords()[0].GetKeyword(), []string{"cat", "kitty"}) {
		t.Errorf("packs %v, keywords %v", ms.GetPacks(), ms.GetKeywords())
	}
	if len(f.ins.updates) != 1 || f.ins.updates[0].GetPredicateName() != mtproto.Predicate_updateNewStickerSet ||
		f.ins.updates[0].GetStickerset_MESSAGES_STICKERSET().GetSet().GetInstalledDate() == nil {
		t.Errorf("the account's devices got %v, want the set installed", f.ins.updates)
	}

	// Anyone opens it by its link; only the account that made it is its creator.
	byName := &mtproto.TLMessagesGetStickerSet{Stickerset: mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "Moi_kotiki"}).To_InputStickerSet()}
	bob := f.as(2)
	if got, err := bob.MessagesGetStickerSet(byName); err != nil || got.GetSet().GetCreator() || got.GetSet().GetInstalledDate() != nil || len(got.GetDocuments()) != 1 {
		t.Errorf("the set as another account gets it: %v, %v", got.GetSet(), err)
	}
	if got, err := c.MessagesGetAllStickers(&mtproto.TLMessagesGetAllStickers{}); err != nil || len(got.GetSets()) != 1 || !got.GetSets()[0].GetCreator() {
		t.Errorf("the account's sets: %v, %v", got.GetSets(), err)
	}

	// A second set with the same title gets another name; a taken name is refused.
	other, err := c.StickersCreateStickerSet(&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "Мои котики", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}})
	if err != nil || other.GetSet().GetShortName() == "Moi_kotiki" || !usersets.ValidShortName(other.GetSet().GetShortName()) {
		t.Fatalf("a second set of the title: %v, %v", other.GetSet().GetShortName(), err)
	}
	if err := create(&mtproto.TLStickersCreateStickerSet{UserId: self, Title: "T", ShortName: "Moi_kotiki", Stickers: []*mtproto.InputStickerSetItem{item(cat1, "😺")}}); !errors.Is(err, mtproto.ErrPackShortNameOccupied) {
		t.Errorf("a set with a taken short name: %v", err)
	}
	if _, err := c.StickersCheckShortName(&mtproto.TLStickersCheckShortName{ShortName: "Moi_kotiki"}); !errors.Is(err, mtproto.ErrShortNameOccupied) {
		t.Errorf("stickers.checkShortName of a taken name: %v", err)
	}
	if _, err := c.StickersCheckShortName(&mtproto.TLStickersCheckShortName{ShortName: "мои"}); !errors.Is(err, mtproto.ErrShortNameInvalid) {
		t.Errorf("stickers.checkShortName of no name: %v", err)
	}
	if ok, err := c.StickersCheckShortName(&mtproto.TLStickersCheckShortName{ShortName: "free_name"}); err != nil || !mtproto.FromBool(ok) {
		t.Errorf("stickers.checkShortName of a free name: %v, %v", ok, err)
	}
	if got, err := c.StickersSuggestShortName(&mtproto.TLStickersSuggestShortName{Title: "Мои котики"}); err != nil || !usersets.ValidShortName(got.GetShortName()) ||
		got.GetShortName() == "Moi_kotiki" || got.GetShortName() == other.GetSet().GetShortName() {
		t.Errorf("stickers.suggestShortName: %v, %v", got, err)
	}
	if got, err := c.StickersSuggestShortName(&mtproto.TLStickersSuggestShortName{Title: "😺"}); err != nil || !usersets.ValidShortName(got.GetShortName()) {
		t.Errorf("stickers.suggestShortName of a title without letters: %v, %v", got, err)
	}

	// Adding: at the end, once per document, by the owner alone.
	f.ins.changed = nil
	added, err := c.StickersAddStickerToSet(&mtproto.TLStickersAddStickerToSet{Stickerset: setRef(ms), Sticker: item(cat2, "🐶")})
	if err != nil || added.GetSet().GetCount() != 2 || added.GetSet().GetHash() == set.GetHash() || added.GetDocuments()[1].GetAttributes()[0].GetH() != 400 {
		t.Fatalf("stickers.addStickerToSet: %v, %v", added.GetSet(), err)
	}
	if len(f.ins.changed) != 1 || f.ins.changed[0] != (installed.Ref{SetID: set.GetId(), Kind: installed.Stickers}) {
		t.Errorf("the accounts that have the set were told %v", f.ins.changed)
	}
	second := added.GetDocuments()[1]
	stored := f.files.next
	for _, again := range []*mtproto.InputDocument{cat2, inputOf(second)} {
		if got, err := c.StickersAddStickerToSet(&mtproto.TLStickersAddStickerToSet{Stickerset: setRef(ms), Sticker: item(again, "🐶")}); err != nil || got.GetSet().GetCount() != 2 {
			t.Errorf("a sticker added twice: %v, %v", got.GetSet(), err)
		}
	}
	if f.files.next != stored || len(f.ins.changed) != 1 {
		t.Errorf("a sticker the set has was stored or told of again")
	}
	if _, err := bob.StickersAddStickerToSet(&mtproto.TLStickersAddStickerToSet{Stickerset: setRef(ms), Sticker: item(cat2, "🐶")}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Errorf("stickers.addStickerToSet by another account: %v", err)
	}
	catalogSet := setOf(t, 77, "Catalog", 3, false)
	f.cat.sets = append(f.cat.sets, catalogSet)
	if _, err := c.StickersAddStickerToSet(&mtproto.TLStickersAddStickerToSet{
		Stickerset: mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 77, AccessHash: 78}).To_InputStickerSet(), Sticker: item(cat2, "🐶"),
	}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Errorf("stickers.addStickerToSet to a set of the catalog: %v", err)
	}

	// Order, emoji, replacement, title.
	moved, err := c.StickersChangeStickerPosition(&mtproto.TLStickersChangeStickerPosition{Sticker: inputOf(second), Position: 0})
	if err != nil || !slices.Equal(ids(moved), []int64{second.GetId(), first.GetId()}) {
		t.Fatalf("stickers.changeStickerPosition: %v, %v", ids(moved), err)
	}
	if _, err := bob.StickersChangeStickerPosition(&mtproto.TLStickersChangeStickerPosition{Sticker: inputOf(second), Position: 1}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Errorf("stickers.changeStickerPosition by another account: %v", err)
	}
	if _, err := c.StickersChangeStickerPosition(&mtproto.TLStickersChangeStickerPosition{Sticker: cat2, Position: 1}); !errors.Is(err, mtproto.ErrStickerInvalid) {
		t.Errorf("stickers.changeStickerPosition of the uploaded document: %v", err)
	}
	changed, err := c.StickersChangeSticker(&mtproto.TLStickersChangeSticker{Sticker: inputOf(second), Emoji: mtproto.MakeFlagsString("🦊"), Keywords: mtproto.MakeFlagsString("fox")})
	if err != nil || changed.GetPacks()[0].GetEmoticon() != "🦊" || changed.GetDocuments()[0].GetAttributes()[1].GetAlt() != "🦊" {
		t.Errorf("stickers.changeSticker: %v, %v", changed.GetPacks(), err)
	}
	replaced, err := c.StickersReplaceSticker(&mtproto.TLStickersReplaceSticker{Sticker: inputOf(second), NewSticker: item(f.files.upload(3, stickerfile.SampleWebp(512, 512)), "🐱")})
	if err != nil || len(replaced.GetDocuments()) != 2 || replaced.GetDocuments()[0].GetId() == second.GetId() || replaced.GetDocuments()[1].GetId() != first.GetId() ||
		replaced.GetPacks()[0].GetEmoticon() != "🐱" {
		t.Fatalf("stickers.replaceSticker: %v, %v", ids(replaced), err)
	}
	if _, err := c.StickersRemoveStickerFromSet(&mtproto.TLStickersRemoveStickerFromSet{Sticker: inputOf(second)}); !errors.Is(err, mtproto.ErrStickerInvalid) {
		t.Errorf("stickers.removeStickerFromSet of a replaced sticker: %v", err)
	}
	renamed, err := c.StickersRenameStickerSet(&mtproto.TLStickersRenameStickerSet{Stickerset: setRef(ms), Title: "Cats"})
	if err != nil || renamed.GetSet().GetTitle() != "Cats" || renamed.GetSet().GetShortName() != "Moi_kotiki" {
		t.Errorf("stickers.renameStickerSet: %v, %v", renamed.GetSet(), err)
	}
	if _, err := bob.StickersRenameStickerSet(&mtproto.TLStickersRenameStickerSet{Stickerset: setRef(ms), Title: "Dogs"}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Errorf("stickers.renameStickerSet by another account: %v", err)
	}

	// The set's own thumbnail, and its removal.
	thumbed, err := c.StickersSetStickerSetThumb(&mtproto.TLStickersSetStickerSetThumb{Stickerset: setRef(ms), Thumb: f.files.upload(4, stickerfile.SampleWebp(100, 100))})
	if err != nil || len(thumbed.GetSet().GetThumbs()) != 1 || thumbed.GetSet().GetThumbs()[0].GetType() != "s" || thumbed.GetSet().GetThumbVersion().GetValue() != 1 {
		t.Fatalf("stickers.setStickerSetThumb: %v, %v", thumbed.GetSet(), err)
	}
	stored1, _ := f.cat.SetByID(context.Background(), set.GetId())
	if stored1.ThumbDocumentID == 0 || f.files.files[stored1.ThumbDocumentID] == nil {
		t.Errorf("the thumbnail's document: %d", stored1.ThumbDocumentID)
	}
	if got, err := c.StickersSetStickerSetThumb(&mtproto.TLStickersSetStickerSetThumb{Stickerset: setRef(ms)}); err != nil || len(got.GetSet().GetThumbs()) != 0 {
		t.Errorf("the thumbnail taken away: %v, %v", got.GetSet(), err)
	}
	if _, err := c.StickersSetStickerSetThumb(&mtproto.TLStickersSetStickerSetThumb{Stickerset: setRef(ms), Thumb: f.files.upload(5, []byte("not an image at all, just some text"))}); !errors.Is(err, mtproto.ErrStickerThumbPngNopng) {
		t.Errorf("a thumbnail of no image: %v", err)
	}

	// The account's sets, the last made first, in pages.
	mine, err := c.MessagesGetMyStickers(&mtproto.TLMessagesGetMyStickers{Limit: 1})
	if err != nil || mine.GetCount() != 2 || len(mine.GetSets()) != 1 || mine.GetSets()[0].GetSet().GetId() != other.GetSet().GetId() ||
		mine.GetSets()[0].GetPredicateName() != mtproto.Predicate_stickerSetCovered || mine.GetSets()[0].GetCover() == nil {
		t.Fatalf("messages.getMyStickers: %v, %v", mine, err)
	}
	next, err := c.MessagesGetMyStickers(&mtproto.TLMessagesGetMyStickers{OffsetId: other.GetSet().GetId(), Limit: 100})
	if err != nil || len(next.GetSets()) != 1 || next.GetSets()[0].GetSet().GetId() != set.GetId() || !next.GetSets()[0].GetSet().GetCreator() {
		t.Errorf("the next page of messages.getMyStickers: %v, %v", next, err)
	}
	if got, _ := bob.MessagesGetMyStickers(&mtproto.TLMessagesGetMyStickers{Limit: 100}); got.GetCount() != 0 || len(got.GetSets()) != 0 {
		t.Errorf("the sets of an account that made none: %v", got)
	}

	// Removing down to an empty set, which stays; deleting it for everyone.
	for _, d := range replaced.GetDocuments() {
		if ms, err = c.StickersRemoveStickerFromSet(&mtproto.TLStickersRemoveStickerFromSet{Sticker: inputOf(d)}); err != nil {
			t.Fatalf("stickers.removeStickerFromSet: %v", err)
		}
	}
	if ms.GetSet().GetCount() != 0 || len(ms.GetPacks()) != 0 {
		t.Errorf("the emptied set: %v", ms)
	}
	if got, err := bob.MessagesGetStickerSet(byName); err != nil || len(got.GetDocuments()) != 0 {
		t.Errorf("the emptied set by its link: %v, %v", got, err)
	}
	if _, err := bob.StickersDeleteStickerSet(&mtproto.TLStickersDeleteStickerSet{Stickerset: setRef(ms)}); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Errorf("stickers.deleteStickerSet by another account: %v", err)
	}
	if ok, err := c.StickersDeleteStickerSet(&mtproto.TLStickersDeleteStickerSet{Stickerset: setRef(ms)}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("stickers.deleteStickerSet: %v", err)
	}
	if _, err := bob.MessagesGetStickerSet(byName); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Errorf("a deleted set by its link: %v", err)
	}
	if len(f.ins.dropped) != 1 || f.ins.dropped[0].SetID != set.GetId() {
		t.Errorf("the deleted set was taken from %v", f.ins.dropped)
	}
	if got, _ := c.MessagesGetAllStickers(&mtproto.TLMessagesGetAllStickers{}); len(got.GetSets()) != 1 {
		t.Errorf("the account's sets after the deletion: %v", got.GetSets())
	}
}

// A set holds no more than its kind may: the sticker past the last is
// refused before its file is stored.
func TestOwnStickerSetFull(t *testing.T) {
	f := newOwnCore(t)
	ms := usersets.New(5, 6, "Full", "full", installed.Stickers, false)
	for i := 0; i < usersets.MaxStickers; i++ {
		usersets.Insert(ms, i, usersets.Sticker{Document: mtproto.MakeTLDocument(&mtproto.Document{Id: int64(100 + i)}).To_Document(), Emoji: []string{"😺"}})
	}
	if _, err := f.sets.Create(context.Background(), 1, ms, 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	stored := f.files.next
	_, err := f.c.StickersAddStickerToSet(&mtproto.TLStickersAddStickerToSet{Stickerset: setRef(ms), Sticker: item(f.files.upload(1, stickerfile.SampleWebp(512, 512)), "😺")})
	if !errors.Is(err, mtproto.ErrStickerpackStickersTooMuch) || f.files.next != stored {
		t.Errorf("a sticker added to a full set: %v, %d documents stored", err, f.files.next-stored)
	}
	many := make([]*mtproto.InputStickerSetItem, usersets.MaxStickers+1)
	if _, err := f.c.StickersCreateStickerSet(&mtproto.TLStickersCreateStickerSet{UserId: mtproto.MakeTLInputUserSelf(nil).To_InputUser(), Title: "T", Stickers: many}); !errors.Is(err, mtproto.ErrStickersTooMuch) {
		t.Errorf("a set of %d stickers: %v", len(many), err)
	}
}

// A set of custom emoji: its emoji are 100x100, documents with
// documentAttributeCustomEmoji, and its thumbnail one of them.
func TestOwnEmojiSet(t *testing.T) {
	f := newOwnCore(t)
	c := f.c
	ms, err := c.StickersCreateStickerSet(&mtproto.TLStickersCreateStickerSet{
		UserId: mtproto.MakeTLInputUserSelf(nil).To_InputUser(), Emojis: true, TextColor: true, Title: "Emo", ShortName: "emo",
		Stickers: []*mtproto.InputStickerSetItem{item(f.files.upload(1, stickerfile.SampleWebp(100, 100)), "😺"), item(f.files.upload(2, stickerfile.SampleWebm(100, 100, "V_VP9", 2)), "🐶")},
	})
	if err != nil {
		t.Fatalf("stickers.createStickerSet of custom emoji: %v", err)
	}
	if !ms.GetSet().GetEmojis() || !ms.GetSet().GetTextColor() || ms.GetSet().GetCount() != 2 {
		t.Fatalf("the set: %v", ms.GetSet())
	}
	a := ms.GetDocuments()[0].GetAttributes()[1]
	if a.GetPredicateName() != mtproto.Predicate_documentAttributeCustomEmoji || !a.GetTextColor() || a.GetStickerset().GetId() != ms.GetSet().GetId() {
		t.Errorf("the attribute of a custom emoji: %v", a)
	}
	video := ms.GetDocuments()[1]
	if video.GetMimeType() != "video/webm" || !slices.ContainsFunc(video.GetAttributes(), func(a *mtproto.DocumentAttribute) bool {
		return a.GetPredicateName() == mtproto.Predicate_documentAttributeVideo && a.GetW() == 100
	}) {
		t.Errorf("a video custom emoji: %v", video)
	}
	if e := f.ins.rows[1]; len(e) != 1 || e[0].Kind != installed.Emojis {
		t.Errorf("the set installed as %v", e)
	}
	got, err := c.StickersSetStickerSetThumb(&mtproto.TLStickersSetStickerSetThumb{Stickerset: setRef(ms), ThumbDocumentId: mtproto.MakeFlagsInt64(video.GetId())})
	if err != nil || got.GetSet().GetThumbDocumentId().GetValue() != video.GetId() {
		t.Errorf("stickers.setStickerSetThumb with an emoji of the set: %v, %v", got.GetSet(), err)
	}
	if _, err := c.StickersSetStickerSetThumb(&mtproto.TLStickersSetStickerSetThumb{Stickerset: setRef(ms), ThumbDocumentId: mtproto.MakeFlagsInt64(12345)}); !errors.Is(err, mtproto.ErrStickerInvalid) {
		t.Errorf("stickers.setStickerSetThumb with an emoji of another set: %v", err)
	}
	if mine, _ := c.MessagesGetMyStickers(&mtproto.TLMessagesGetMyStickers{Limit: 10}); len(mine.GetSets()) != 1 || mine.GetSets()[0].GetPredicateName() != mtproto.Predicate_stickerSetFullCovered {
		t.Errorf("a set of custom emoji among the account's: %v", mine.GetSets())
	}
}

// Search over every stored set, a user's among the catalog's: sets by
// title, stickers by emoji, by keyword and by the word of an emoji.
func TestStickerSearch(t *testing.T) {
	f := newOwnCore(t)
	c := f.c
	if _, err := c.StickersCreateStickerSet(&mtproto.TLStickersCreateStickerSet{
		UserId: mtproto.MakeTLInputUserSelf(nil).To_InputUser(), Title: "Space Cats", ShortName: "space_cats",
		Stickers: []*mtproto.InputStickerSetItem{
			mtproto.MakeTLInputStickerSetItem(&mtproto.InputStickerSetItem{Document: f.files.upload(1, stickerfile.SampleWebp(512, 512)), Emoji: "😺", Keywords: mtproto.MakeFlagsString("astronaut")}).To_InputStickerSetItem(),
			item(f.files.upload(2, stickerfile.SampleWebp(512, 512)), "🚀"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.as(2).StickersCreateStickerSet(&mtproto.TLStickersCreateStickerSet{
		UserId: mtproto.MakeTLInputUserSelf(nil).To_InputUser(), Emojis: true, Title: "Cat Emoji", ShortName: "cat_emoji",
		Stickers: []*mtproto.InputStickerSetItem{item(f.files.upload(3, stickerfile.SampleWebp(100, 100)), "😺")},
	}); err != nil {
		t.Fatal(err)
	}
	put(t, f.cat, "messages.getEmojiKeywords", "en", 1, &mtproto.EmojiKeywordsDifference{Keywords: []*mtproto.EmojiKeyword{
		{PredicateName: mtproto.Predicate_emojiKeyword, Keyword: "rocket", Emoticons: []string{"🚀"}},
	}})
	bob := f.as(2)

	sets, err := bob.MessagesSearchStickerSets(&mtproto.TLMessagesSearchStickerSets{Q: "cat"})
	if err != nil || len(sets.GetSets()) != 1 || sets.GetSets()[0].GetSet().GetShortName() != "space_cats" || sets.GetSets()[0].GetSet().GetCreator() ||
		sets.GetSets()[0].GetPredicateName() != mtproto.Predicate_stickerSetMultiCovered || len(sets.GetSets()[0].GetCovers()) != 2 {
		t.Fatalf("messages.searchStickerSets: %v, %v", sets, err)
	}
	if again, _ := bob.MessagesSearchStickerSets(&mtproto.TLMessagesSearchStickerSets{Q: "cat", Hash: sets.GetHash()}); again.GetPredicateName() != mtproto.Predicate_messages_foundStickerSetsNotModified {
		t.Errorf("messages.searchStickerSets with the hash of the answer: %s", again.GetPredicateName())
	}
	emo, err := bob.MessagesSearchEmojiStickerSets(&mtproto.TLMessagesSearchEmojiStickerSets{Q: "cat"})
	if err != nil || len(emo.GetSets()) != 1 || emo.GetSets()[0].GetPredicateName() != mtproto.Predicate_stickerSetFullCovered || !emo.GetSets()[0].GetSet().GetCreator() {
		t.Errorf("messages.searchEmojiStickerSets: %v, %v", emo, err)
	}
	if none, _ := bob.MessagesSearchStickerSets(&mtproto.TLMessagesSearchStickerSets{Q: "dogs"}); none.GetPredicateName() != mtproto.Predicate_messages_foundStickerSets || len(none.GetSets()) != 0 {
		t.Errorf("messages.searchStickerSets for nothing: %v", none)
	}

	for q, want := range map[[2]string]int{
		{"", "😺"}: 1, {"astro", ""}: 1, {"rocket", ""}: 1, {"", "😺🚀"}: 2, {"😺", ""}: 1, {"dog", ""}: 0, {"", ""}: 0,
	} {
		got, err := bob.MessagesSearchStickers(&mtproto.TLMessagesSearchStickers{Q: q[0], Emoticon: q[1], LangCode: []string{"en"}, Limit: 50})
		if err != nil || len(got.GetStickers()) != want || got.GetNextOffset() != nil {
			t.Errorf("messages.searchStickers for %q: %d stickers, next %v, %v; want %d", q, len(got.GetStickers()), got.GetNextOffset(), err, want)
		}
	}
	page, err := bob.MessagesSearchStickers(&mtproto.TLMessagesSearchStickers{Emoticon: "😺🚀", Limit: 1})
	if err != nil || len(page.GetStickers()) != 1 || page.GetNextOffset().GetValue() != 1 {
		t.Fatalf("a page of messages.searchStickers: %v, %v", page, err)
	}
	rest, _ := bob.MessagesSearchStickers(&mtproto.TLMessagesSearchStickers{Emoticon: "😺🚀", Offset: 1, Limit: 1})
	if len(rest.GetStickers()) != 1 || rest.GetStickers()[0].GetId() == page.GetStickers()[0].GetId() || rest.GetNextOffset() != nil {
		t.Errorf("the next page: %v", rest)
	}
	if same, _ := bob.MessagesSearchStickers(&mtproto.TLMessagesSearchStickers{Emoticon: "😺🚀", Limit: 1, Hash: page.GetHash()}); same.GetPredicateName() != mtproto.Predicate_messages_foundStickersNotModified || same.GetNextOffset().GetValue() != 1 {
		t.Errorf("messages.searchStickers with the hash of the page: %v", same)
	}
	custom, err := bob.MessagesSearchStickers(&mtproto.TLMessagesSearchStickers{Emojis: true, Emoticon: "😺", Limit: 50})
	if err != nil || len(custom.GetStickers()) != 1 || custom.GetStickers()[0].GetMimeType() != "image/webp" {
		t.Errorf("messages.searchStickers of custom emoji: %v, %v", custom, err)
	}
	list, err := bob.MessagesSearchCustomEmoji(&mtproto.TLMessagesSearchCustomEmoji{Emoticon: "😺"})
	if err != nil || len(list.GetDocumentId()) != 1 || list.GetDocumentId()[0] != custom.GetStickers()[0].GetId() {
		t.Errorf("messages.searchCustomEmoji: %v, %v", list, err)
	}
	if same, _ := bob.MessagesSearchCustomEmoji(&mtproto.TLMessagesSearchCustomEmoji{Emoticon: "😺", Hash: list.GetHash()}); same.GetPredicateName() != mtproto.Predicate_emojiListNotModified {
		t.Errorf("messages.searchCustomEmoji with the hash of the list: %s", same.GetPredicateName())
	}
}

// The trending sets an account has not seen are its unread ones, until it
// reads them; an id that is no trending set is not kept.
func TestReadFeaturedStickers(t *testing.T) {
	f := newOwnCore(t)
	c := f.c
	covered := func(ids ...int64) []*mtproto.StickerSetCovered {
		var out []*mtproto.StickerSetCovered
		for _, id := range ids {
			out = append(out, mtproto.MakeTLStickerSetCovered(&mtproto.StickerSetCovered{Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: id}).To_StickerSet()}).To_StickerSetCovered())
		}
		return out
	}
	put(t, f.cat, "messages.getFeaturedStickers", "", 50, mtproto.MakeTLMessagesFeaturedStickers(&mtproto.Messages_FeaturedStickers{Hash: 50, Count: 2, Sets: covered(10, 20), Unread: []int64{}}).To_Messages_FeaturedStickers())
	put(t, f.cat, "messages.getFeaturedEmojiStickers", "", 60, mtproto.MakeTLMessagesFeaturedStickers(&mtproto.Messages_FeaturedStickers{Hash: 60, Count: 1, Sets: covered(30), Unread: []int64{}}).To_Messages_FeaturedStickers())

	got, err := c.MessagesGetFeaturedStickers(&mtproto.TLMessagesGetFeaturedStickers{Hash: 50})
	if err != nil || !slices.Equal(got.GetUnread(), []int64{10, 20}) || got.GetHash() == 50 {
		t.Fatalf("the trending sets of an account that saw none: unread %v, hash %d, %v", got.GetUnread(), got.GetHash(), err)
	}
	if again, _ := c.MessagesGetFeaturedStickers(&mtproto.TLMessagesGetFeaturedStickers{Hash: got.GetHash()}); again.GetPredicateName() != mtproto.Predicate_messages_featuredStickersNotModified {
		t.Errorf("the trending sets with the hash of the answer: %s", again.GetPredicateName())
	}
	if ok, err := c.MessagesReadFeaturedStickers(&mtproto.TLMessagesReadFeaturedStickers{Id: []int64{10, 30, 999, 10}}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("messages.readFeaturedStickers: %v", err)
	}
	if !slices.Equal(f.ins.readStickers, []int64{10}) || !slices.Equal(f.ins.readEmojis, []int64{30}) {
		t.Errorf("read as sticker sets %v, as emoji sets %v", f.ins.readStickers, f.ins.readEmojis)
	}
	if got, _ := c.MessagesGetFeaturedStickers(&mtproto.TLMessagesGetFeaturedStickers{}); !slices.Equal(got.GetUnread(), []int64{20}) {
		t.Errorf("unread after reading one: %v", got.GetUnread())
	}
	if got, _ := c.MessagesGetFeaturedEmojiStickers(&mtproto.TLMessagesGetFeaturedEmojiStickers{Hash: 60}); got.GetPredicateName() != mtproto.Predicate_messages_featuredStickersNotModified {
		t.Errorf("the trending emoji sets, all seen, with the stored hash: %s", got.GetPredicateName())
	}
	if got, _ := f.as(2).MessagesGetFeaturedEmojiStickers(&mtproto.TLMessagesGetFeaturedEmojiStickers{}); !slices.Equal(got.GetUnread(), []int64{30}) {
		t.Errorf("unread of another account: %v", got.GetUnread())
	}
	// No ids: all the trending sets, as Android reads them.
	bob := f.as(2)
	if _, err := bob.MessagesReadFeaturedStickers(&mtproto.TLMessagesReadFeaturedStickers{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ins.readStickers, []int64{10, 20}) || !slices.Equal(f.ins.readEmojis, []int64{30}) {
		t.Errorf("all read as sticker sets %v, as emoji sets %v", f.ins.readStickers, f.ins.readEmojis)
	}
	if got, _ := bob.MessagesGetFeaturedStickers(&mtproto.TLMessagesGetFeaturedStickers{}); len(got.GetUnread()) != 0 || got.GetHash() != 50 {
		t.Errorf("the trending sets after reading all: unread %v, hash %d", got.GetUnread(), got.GetHash())
	}
}

// memAttached is the stickers put on the media of a test.
type memAttached map[int64][]int64

func (m memAttached) Of(_ context.Context, id int64) ([]int64, error) { return m[id], nil }

// The sets of the stickers put on a photo, each once, in their order.
func TestGetAttachedStickers(t *testing.T) {
	f := newOwnCore(t)
	f.cat.sets = append(f.cat.sets, setOf(t, 50, "Cats", 1, false), setOf(t, 60, "Dogs", 1, false))
	md := f.c.svcCtx.Media.(*stickerMedia)
	md.docs[501], md.docs[502], md.docs[601], md.docs[701] = stickerDoc(501, "😺", 50), stickerDoc(502, "😺", 50), stickerDoc(601, "🐶", 60), stickerDoc(701, "🦊", 70)
	f.c.svcCtx.Attached = memAttached{9: {601, 501, 502, 701, 404}}
	photo := func(id int64) *mtproto.TLMessagesGetAttachedStickers {
		return &mtproto.TLMessagesGetAttachedStickers{Media: mtproto.MakeTLInputStickeredMediaPhoto(&mtproto.InputStickeredMedia{
			Id_INPUTPHOTO: mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: id}).To_InputPhoto(),
		}).To_InputStickeredMedia()}
	}
	got, err := f.c.MessagesGetAttachedStickers(photo(9))
	if err != nil || len(got.GetDatas()) != 2 || got.GetDatas()[0].GetSet().GetId() != 60 || got.GetDatas()[1].GetSet().GetId() != 50 || got.GetDatas()[0].GetCover() == nil {
		t.Errorf("messages.getAttachedStickers: %v, %v", got.GetDatas(), err)
	}
	if got, err := f.c.MessagesGetAttachedStickers(photo(8)); err != nil || got.GetDatas() == nil || len(got.GetDatas()) != 0 {
		t.Errorf("messages.getAttachedStickers of a photo without stickers: %v, %v", got, err)
	}
	if _, err := f.c.MessagesGetAttachedStickers(&mtproto.TLMessagesGetAttachedStickers{}); !errors.Is(err, mtproto.ErrMediaInvalid) {
		t.Errorf("messages.getAttachedStickers of nothing: %v", err)
	}
}
