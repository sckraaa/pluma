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
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
	"google.golang.org/protobuf/proto"
)

// memStickerLists is the lists of stickers of a test, newest first, trimmed
// to their limits as stickerlists.Store keeps them.
type memStickerLists struct {
	lists map[stickerlists.List][]stickerlists.Entry
	now   int64
}

func (m *memStickerLists) Entries(_ context.Context, _ int64, list stickerlists.List) ([]stickerlists.Entry, error) {
	return slices.Clone(m.lists[list]), nil
}

func (m *memStickerLists) Add(_ context.Context, _ int64, list stickerlists.List, id int64) error {
	m.now++
	l := slices.DeleteFunc(m.lists[list], func(e stickerlists.Entry) bool { return e.DocumentID == id })
	l = append([]stickerlists.Entry{{DocumentID: id, UsedAt: m.now}}, l...)
	m.lists[list] = l[:min(len(l), stickerlists.Limit(list))]
	return nil
}

func (m *memStickerLists) Remove(_ context.Context, _ int64, list stickerlists.List, id int64) error {
	m.lists[list] = slices.DeleteFunc(m.lists[list], func(e stickerlists.Entry) bool { return e.DocumentID == id })
	return nil
}

func (m *memStickerLists) Clear(_ context.Context, _ int64, list stickerlists.List) error {
	delete(m.lists, list)
	return nil
}

// stickerMedia has the documents of a test.
type stickerMedia struct {
	media_client.MediaClient
	docs map[int64]*mtproto.Document
}

func (f *stickerMedia) MediaGetDocument(_ context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error) {
	if d, ok := f.docs[in.GetId()]; ok {
		return d, nil
	}
	return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: in.GetId()}).To_Document(), nil
}

func (f *stickerMedia) MediaGetDocumentList(_ context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error) {
	out := &media.Vector_Document{Datas: []*mtproto.Document{}}
	for _, id := range in.GetIdList() {
		if d, ok := f.docs[id]; ok {
			out.Datas = append(out.Datas, d)
		}
	}
	return out, nil
}

func stickerDoc(id int64, alt string, setID int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id:         id,
		AccessHash: id * 3,
		MimeType:   "image/webp",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
				Alt:        alt,
				Stickerset: mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: setID, AccessHash: setID + 1}).To_InputStickerSet(),
			}).To_DocumentAttribute(),
		},
	}).To_Document()
}

func inputDoc(id, accessHash int64) *mtproto.InputDocument {
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: id, AccessHash: accessHash, FileReference: []byte{}}).To_InputDocument()
}

func newListsCore(t *testing.T) (*CatalogCore, *memStickerLists, *stickerMedia) {
	t.Helper()
	// Set 50 of the catalog names sticker 1 by two emoji.
	ms := mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 50, AccessHash: 51, ShortName: "Cats"}).To_StickerSet(),
		Packs: []*mtproto.StickerPack{
			mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "😺", Documents: []int64{1}}).To_StickerPack(),
			mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "❤", Documents: []int64{1}}).To_StickerPack(),
		},
	}).To_Messages_StickerSet()
	b, err := proto.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	cat := &memCatalog{sets: []*catalog.Set{{ID: 50, AccessHash: 51, ShortName: "Cats", Data: b}}}
	lists := &memStickerLists{lists: map[stickerlists.List][]stickerlists.Entry{}}
	md := &stickerMedia{docs: map[int64]*mtproto.Document{
		1: stickerDoc(1, "😺", 50),
		2: stickerDoc(2, "🐶", 60), // of a set the catalog has not
		3: stickerDoc(3, "🦉", 60),
		9: mtproto.MakeTLDocument(&mtproto.Document{Id: 9, AccessHash: 27, MimeType: "image/gif"}).To_Document(),
	}}
	c := New(context.Background(), &svc.ServiceContext{Catalog: cat, Media: md, StickerLists: lists})
	c.MD = &metadata.RpcMetadata{UserId: 42}
	return c, lists, md
}

func docIDs(docs []*mtproto.Document) []int64 {
	out := []int64{}
	for _, d := range docs {
		out = append(out, d.GetId())
	}
	return out
}

// A faved sticker goes to the top of the list, the oldest past the limit
// out; the list comes with the packs of its emoji (the set's, else the
// sticker's alt) and the clients' hash, for which it is not modified.
func TestFavedStickers(t *testing.T) {
	c, _, _ := newListsCore(t)
	for _, id := range []int64{2, 1, 3, 2} {
		if r, err := c.MessagesFaveSticker(&mtproto.TLMessagesFaveSticker{Id: inputDoc(id, id*3), Unfave: mtproto.BoolFalse}); err != nil || !mtproto.FromBool(r) {
			t.Fatalf("fave %d: %v, %v", id, r, err)
		}
	}
	r, err := c.MessagesGetFavedStickers(&mtproto.TLMessagesGetFavedStickers{})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_favedStickers {
		t.Fatalf("answer %s", r.GetPredicateName())
	}
	if got := docIDs(r.GetStickers()); !slices.Equal(got, []int64{2, 3, 1}) {
		t.Errorf("faved %v, want [2 3 1]", got)
	}
	var packs []string
	for _, p := range r.GetPacks() {
		for _, id := range p.GetDocuments() {
			packs = append(packs, p.GetEmoticon()+string(rune('0'+id)))
		}
	}
	if want := []string{"🐶2", "🦉3", "😺1", "❤1"}; !slices.Equal(packs, want) {
		t.Errorf("packs %v, want %v", packs, want)
	}
	if r.GetHash() != stickerlists.Hash(r.GetStickers()) || r.GetHash() == 0 {
		t.Errorf("hash %d", r.GetHash())
	}
	same, err := c.MessagesGetFavedStickers(&mtproto.TLMessagesGetFavedStickers{Hash: r.GetHash()})
	if err != nil || same.GetPredicateName() != mtproto.Predicate_messages_favedStickersNotModified {
		t.Errorf("same hash: %v, %v", same, err)
	}

	// Unfaved by its id alone, even when its document is gone.
	if _, err := c.MessagesFaveSticker(&mtproto.TLMessagesFaveSticker{Id: inputDoc(3, 0), Unfave: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	r, _ = c.MessagesGetFavedStickers(&mtproto.TLMessagesGetFavedStickers{Hash: r.GetHash()})
	if got := docIDs(r.GetStickers()); !slices.Equal(got, []int64{2, 1}) {
		t.Errorf("faved after unfave %v, want [2 1]", got)
	}

	// The oldest past the limit go.
	_, lists, md := newListsCore(t)
	c.svcCtx.StickerLists, c.svcCtx.Media = lists, md
	for id := int64(100); id < 100+stickerlists.FavedLimit+1; id++ {
		md.docs[id] = stickerDoc(id, "", 0)
		if _, err := c.MessagesFaveSticker(&mtproto.TLMessagesFaveSticker{Id: inputDoc(id, id*3), Unfave: mtproto.BoolFalse}); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = c.MessagesGetFavedStickers(&mtproto.TLMessagesGetFavedStickers{})
	if got := docIDs(r.GetStickers()); len(got) != stickerlists.FavedLimit || got[0] != 100+stickerlists.FavedLimit || slices.Contains(got, 100) {
		t.Errorf("faved %v past the limit", got)
	}
}

// Only a sticker with its access hash can be faved or saved; a bot has no
// lists.
func TestStickerIdInvalid(t *testing.T) {
	c, lists, _ := newListsCore(t)
	for name, id := range map[string]*mtproto.InputDocument{
		"no sticker":         inputDoc(9, 27),
		"another hash":       inputDoc(1, 4),
		"gone":               inputDoc(77, 231),
		"inputDocumentEmpty": mtproto.MakeTLInputDocumentEmpty(nil).To_InputDocument(),
	} {
		if _, err := c.MessagesFaveSticker(&mtproto.TLMessagesFaveSticker{Id: id, Unfave: mtproto.BoolFalse}); !errors.Is(err, mtproto.ErrStickerIdInvalid) {
			t.Errorf("fave %s: %v", name, err)
		}
		if _, err := c.MessagesSaveRecentSticker(&mtproto.TLMessagesSaveRecentSticker{Id: id, Unsave: mtproto.BoolFalse}); !errors.Is(err, mtproto.ErrStickerIdInvalid) {
			t.Errorf("save %s: %v", name, err)
		}
	}
	if len(lists.lists) != 0 {
		t.Errorf("lists %v", lists.lists)
	}
	c.MD.IsBot = true
	if _, err := c.MessagesFaveSticker(&mtproto.TLMessagesFaveSticker{Id: inputDoc(1, 3), Unfave: mtproto.BoolFalse}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("fave by a bot: %v", err)
	}
	if _, err := c.MessagesGetRecentStickers(&mtproto.TLMessagesGetRecentStickers{}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("recent of a bot: %v", err)
	}
}

// The recent stickers and those attached to photos are two lists: each
// with the dates of use, each cleared on its own.
func TestRecentStickers(t *testing.T) {
	c, _, _ := newListsCore(t)
	save := func(id int64, attached, unsave bool) {
		t.Helper()
		if _, err := c.MessagesSaveRecentSticker(&mtproto.TLMessagesSaveRecentSticker{Attached: attached, Id: inputDoc(id, id*3), Unsave: mtproto.ToBool(unsave)}); err != nil {
			t.Fatalf("save %d: %v", id, err)
		}
	}
	recent := func(attached bool) *mtproto.Messages_RecentStickers {
		t.Helper()
		r, err := c.MessagesGetRecentStickers(&mtproto.TLMessagesGetRecentStickers{Attached: attached})
		if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_recentStickers {
			t.Fatalf("recent: %v, %v", r, err)
		}
		return r
	}
	save(1, false, false)
	save(2, false, false)
	save(3, true, false)
	save(1, false, false)
	save(2, false, true)
	r := recent(false)
	if got := docIDs(r.GetStickers()); !slices.Equal(got, []int64{1}) {
		t.Errorf("recent %v, want [1]", got)
	}
	if len(r.GetDates()) != len(r.GetStickers()) || r.GetDates()[0] == 0 {
		t.Errorf("dates %v", r.GetDates())
	}
	if got := docIDs(recent(true).GetStickers()); !slices.Equal(got, []int64{3}) {
		t.Errorf("attached %v, want [3]", got)
	}
	if _, err := c.MessagesClearRecentStickers(&mtproto.TLMessagesClearRecentStickers{}); err != nil {
		t.Fatal(err)
	}
	if got := docIDs(recent(false).GetStickers()); len(got) != 0 {
		t.Errorf("recent %v after clear", got)
	}
	if got := docIDs(recent(true).GetStickers()); !slices.Equal(got, []int64{3}) {
		t.Errorf("attached %v after clear of recent", got)
	}
	empty := recent(false)
	if empty.GetHash() != 0 || empty.GetStickers() == nil || empty.GetPacks() == nil || empty.GetDates() == nil {
		t.Errorf("empty list %v", empty)
	}
}
