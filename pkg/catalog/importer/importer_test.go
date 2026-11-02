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

package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// memCatalog is the store of a test.
type memCatalog struct {
	stored     *catalog.Catalog
	docs       map[[32]byte]int64
	ids        map[[2]string][2]int64
	failUpdate bool
	demand     map[string]*catalog.Set
}

func (m *memCatalog) IDFor(_ context.Context, kind, name string, newID func() (int64, int64, error)) (int64, int64, error) {
	if m.ids == nil {
		m.ids = map[[2]string][2]int64{}
	}
	if v, ok := m.ids[[2]string{kind, name}]; ok {
		return v[0], v[1], nil
	}
	id, hash, err := newID()
	if err != nil {
		return 0, 0, err
	}
	m.ids[[2]string{kind, name}] = [2]int64{id, hash}
	return id, hash, nil
}

func (m *memCatalog) DocumentID(_ context.Context, key [32]byte) (int64, error) {
	return m.docs[key], nil
}

func (m *memCatalog) PutDocument(_ context.Context, key [32]byte, id int64) error {
	m.docs[key] = id
	return nil
}

func (m *memCatalog) PutSet(_ context.Context, set *catalog.Set, _ int64) error {
	if m.demand == nil {
		m.demand = map[string]*catalog.Set{}
	}
	m.demand[set.ShortName] = set
	return nil
}

func (m *memCatalog) Replace(_ context.Context, c *catalog.Catalog) error {
	if m.failUpdate {
		return errors.New("the store is down")
	}
	m.stored = c
	return nil
}

// memDocuments stores documents in memory, with ids from 1000 on.
type memDocuments struct {
	byID  map[int64]*mtproto.Document
	files map[int64][]byte
	next  int64
}

func (m *memDocuments) Import(_ context.Context, doc *mtproto.Document, file []byte, thumbs map[string][]byte, keep int64) (*mtproto.Document, error) {
	m.next++
	d := proto.Clone(doc).(*mtproto.Document)
	d.Id, d.AccessHash, d.FileReference, d.DcId = 1000+m.next, 7, []byte{}, 1
	if keep != 0 {
		d.Id = keep
	}
	for _, t := range d.GetThumbs() {
		if t.GetPredicateName() == mtproto.Predicate_photoSize && thumbs[t.GetType()] == nil {
			return nil, fmt.Errorf("thumbnail %s without its file", t.GetType())
		}
	}
	m.byID[d.Id] = d
	m.files[d.Id] = file
	return d, nil
}

func (m *memDocuments) Get(_ context.Context, ids []int64) ([]*mtproto.Document, error) {
	var out []*mtproto.Document
	for _, id := range ids {
		if d := m.byID[id]; d != nil {
			out = append(out, d)
		}
	}
	return out, nil
}

// A sticker of set 777 as tweb gives it.
func twebSticker(id int64, emoji string) map[string]any {
	return map[string]any{
		"_": "document", "pFlags": map[string]any{}, "id": fmt.Sprint(id), "access_hash": "-5", "file_reference": []any{1, 2},
		"date": 1700000000, "mime_type": "application/x-tgsticker", "size": 4, "dc_id": 2,
		"thumbs": []any{
			map[string]any{"_": "photoPathSize", "type": "j", "bytes": []any{9}},
			map[string]any{"_": "photoSize", "type": "m", "w": 128, "h": 128, "size": 2},
		},
		"attributes": []any{
			map[string]any{"_": "documentAttributeSticker", "pFlags": map[string]any{}, "alt": emoji,
				"stickerset": map[string]any{"_": "inputStickerSetID", "id": "777", "access_hash": "8"}},
		},
	}
}

func writeCollection(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const tgA, tgB = int64(5368324170671202286), int64(5368324170671202287)
	set := map[string]any{
		"_": "messages.stickerSet",
		"set": map[string]any{"_": "stickerSet", "pFlags": map[string]any{"emojis": true}, "id": "777", "access_hash": "8", "title": "Animated",
			"short_name": "AnimatedEmojies", "count": 2, "hash": 123, "thumb_document_id": "424242",
			"thumbs":      []any{map[string]any{"_": "photoSize", "type": "s", "w": 100, "h": 100, "size": 10}},
			"thumb_dc_id": 2, "thumb_version": 5},
		"packs": []any{
			map[string]any{"_": "stickerPack", "emoticon": "😀", "documents": []any{fmt.Sprint(tgA)}},
			map[string]any{"_": "stickerPack", "emoticon": "😃", "documents": []any{fmt.Sprint(tgB)}},
		},
		"keywords":  []any{},
		"documents": []any{twebSticker(tgA, "😀"), twebSticker(tgB, "😃")},
	}
	write("sets/777.json", set)
	write("answers/reactions.json", map[string]any{
		"_": "messages.availableReactions", "hash": 99,
		"reactions": []any{map[string]any{
			"_": "availableReaction", "pFlags": map[string]any{}, "reaction": "😀", "title": "Grin",
			"static_icon": twebSticker(tgA, "😀"), "appear_animation": twebSticker(tgB, "😃"), "select_animation": twebSticker(tgA, "😀"),
			"activate_animation": twebSticker(tgB, "😃"), "effect_animation": twebSticker(tgA, "😀"),
		}},
	})
	write("answers/emojilist.json", map[string]any{"_": "emojiList", "hash": 5, "document_id": []any{fmt.Sprint(tgB)}})
	write("wallpapers/w.json", map[string]any{"_": "wallPaperNoFile", "id": "42", "pFlags": map[string]any{"default": true}})
	for _, id := range []int64{tgA, tgB} {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("files/%d", id)), []byte(fmt.Sprintf("%04d", id%10000)), 0o600); err != nil {
			if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("files/%d", id)), []byte(fmt.Sprintf("%04d", id%10000)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("files/%d.m", id)), []byte("mm"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", Manifest{
		Layer: 228, Client: "test", CollectedAt: 1,
		Answers: []AnswerFile{
			{Method: "messages.getAvailableReactions", File: "answers/reactions.json"},
			{Method: "account.getDefaultProfilePhotoEmojis", File: "answers/emojilist.json"},
		},
		Sets:       []SetFile{{File: "sets/777.json", Kind: catalog.KindAnimatedEmoji}},
		WallPapers: []string{"wallpapers/w.json"},
		Files: []DocumentFiles{
			{ID: fmt.Sprint(tgA), File: fmt.Sprintf("files/%d", tgA), Thumbs: map[string]string{"m": fmt.Sprintf("files/%d.m", tgA)}},
			{ID: fmt.Sprint(tgB), File: fmt.Sprintf("files/%d", tgB), Thumbs: map[string]string{"m": fmt.Sprintf("files/%d.m", tgB)}},
		},
	})
	return dir
}

// No id of Telegram is left in what is stored: no integer field of any
// stored object holds one.
func noTelegramIDs(t *testing.T, c *catalog.Catalog) {
	t.Helper()
	tg := map[int64]bool{5368324170671202286: true, 5368324170671202287: true, 777: true, 42: true}
	check := func(name string, m proto.Message) {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			x.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
				if fd.Kind() != protoreflect.Int64Kind {
					return true
				}
				if fd.IsList() {
					for i := 0; i < v.List().Len(); i++ {
						if tg[v.List().Get(i).Int()] {
							t.Errorf("%s: %s holds Telegram's %d", name, fd.FullName(), v.List().Get(i).Int())
						}
					}
				} else if tg[v.Int()] {
					t.Errorf("%s: %s holds Telegram's %d", name, fd.FullName(), v.Int())
				}
				return true
			})
		})
	}
	for key, a := range c.Answers {
		var m proto.Message
		switch key[0] {
		case "messages.getAvailableReactions":
			m = &mtproto.Messages_AvailableReactions{}
		case "account.getDefaultProfilePhotoEmojis":
			m = &mtproto.EmojiList{}
		case "account.getWallPaper":
			m = &mtproto.WallPaper{}
		default:
			t.Fatalf("an answer of %v", key)
		}
		if err := proto.Unmarshal(a.Data, m); err != nil {
			t.Fatal(err)
		}
		check(fmt.Sprint(key), m)
	}
	for _, s := range c.Sets {
		m := &mtproto.Messages_StickerSet{}
		if err := proto.Unmarshal(s.Data, m); err != nil {
			t.Fatal(err)
		}
		check(s.ShortName, m)
	}
}

func TestImport(t *testing.T) {
	dir := writeCollection(t)
	cat := &memCatalog{docs: map[[32]byte]int64{}}
	docs := &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}
	im := &Importer{Catalog: cat, Documents: docs, Now: func() int64 { return 5 }}
	res, err := im.Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewDocuments != 2 || res.Sets != 1 {
		t.Fatalf("result %+v", res)
	}
	noTelegramIDs(t, cat.stored)

	set := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(cat.stored.Sets[0].Data, set); err != nil {
		t.Fatal(err)
	}
	if len(set.GetSet().GetThumbs()) != 0 || set.GetSet().GetThumbVersion() != nil || set.GetSet().GetThumbDcId() != nil || set.GetSet().GetThumbDocumentId() != nil {
		t.Fatalf("the set's own thumbnail stayed: %v", set.GetSet())
	}
	if set.GetSet().GetId() == 777 || set.GetSet().GetHash() == 123 || set.GetSet().GetHash() != cat.stored.Sets[0].Hash ||
		cat.stored.Sets[0].Kind != catalog.KindAnimatedEmoji {
		t.Fatalf("set %v", set.GetSet())
	}
	for _, d := range set.GetDocuments() {
		if d.GetId() < 1001 || d.GetAttributes()[0].GetStickerset().GetId() != set.GetSet().GetId() {
			t.Fatalf("document %v", d)
		}
	}
	if set.GetPacks()[0].GetDocuments()[0] != set.GetDocuments()[0].GetId() && set.GetPacks()[0].GetDocuments()[0] != set.GetDocuments()[1].GetId() {
		t.Fatalf("pack %v", set.GetPacks())
	}

	r := &mtproto.Messages_AvailableReactions{}
	a := cat.stored.Answers[[2]string{"messages.getAvailableReactions", ""}]
	if err := proto.Unmarshal(a.Data, r); err != nil {
		t.Fatal(err)
	}
	if r.GetHash() == 99 || int64(r.GetHash()) != a.Hash || r.GetReactions()[0].GetStaticIcon().GetId() < 1001 {
		t.Fatalf("reactions %v, hash %d", r, a.Hash)
	}
	if _, ok := cat.stored.Answers[[2]string{"account.getWallPaper", "id:42"}]; ok {
		t.Fatal("a wallpaper by Telegram's id")
	}

	// Again: no file stored twice, the same ids.
	first := map[[2]string]catalog.Answer{}
	for k, v := range cat.stored.Answers {
		first[k] = v
	}
	setID := cat.stored.Sets[0].ID
	res, err = im.Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewDocuments != 0 || cat.stored.Sets[0].ID != setID {
		t.Fatalf("again: %+v, set %d then %d", res, setID, cat.stored.Sets[0].ID)
	}
	if cat.stored.Answers[[2]string{"messages.getAvailableReactions", ""}].Hash != first[[2]string{"messages.getAvailableReactions", ""}].Hash {
		t.Fatal("the same catalog, another hash")
	}
}

func TestImportRefusesBrokenCollections(t *testing.T) {
	for name, edit := range map[string]func(dir string){
		"another layer": func(dir string) { rewriteManifest(t, dir, func(m *Manifest) { m.Layer = 229 }) },
		"a missing file": func(dir string) {
			rewriteManifest(t, dir, func(m *Manifest) { m.Files = m.Files[:1] })
		},
		"a thumbnail without its file": func(dir string) {
			rewriteManifest(t, dir, func(m *Manifest) { m.Files[0].Thumbs = nil })
		},
		"a file outside": func(dir string) {
			rewriteManifest(t, dir, func(m *Manifest) { m.Answers[0].File = "../x.json" })
		},
		"a set it names left out, by its stickers' pack": func(dir string) {
			// The set's packs and stickers name it as a stickerSet of the
			// answers it is missing from: no document refers to it then,
			// but the featured answer does.
			rewriteManifest(t, dir, func(m *Manifest) {
				m.Sets = nil
				m.Answers = append(m.Answers, AnswerFile{Method: "messages.getFeaturedStickers", File: "answers/featured.json"})
			})
			b, _ := json.Marshal(map[string]any{"_": "messages.featuredStickers", "pFlags": map[string]any{}, "hash": 1, "count": 1,
				"sets": []any{map[string]any{"_": "stickerSetCovered",
					"set":   map[string]any{"_": "stickerSet", "pFlags": map[string]any{}, "id": "777", "access_hash": "8", "title": "A", "short_name": "AnimatedEmojies", "count": 2, "hash": 1},
					"cover": map[string]any{"_": "documentEmpty", "id": "1"}}},
				"unread": []any{}})
			if err := os.WriteFile(filepath.Join(dir, "answers/featured.json"), b, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		dir := writeCollection(t)
		edit(dir)
		cat := &memCatalog{docs: map[[32]byte]int64{}}
		im := &Importer{Catalog: cat, Documents: &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}}
		if _, err := im.Run(context.Background(), dir); err == nil {
			t.Errorf("%s: imported", name)
		}
		if cat.stored != nil {
			t.Errorf("%s: a catalog stored", name)
		}
	}
}

func rewriteManifest(t *testing.T, dir string, f func(*Manifest)) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manifest{}
	if err := json.Unmarshal(b, m); err != nil {
		t.Fatal(err)
	}
	f(m)
	b, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A document clients name by id keeps Telegram's id; the others do not.
func TestImportKeepsNamedIDs(t *testing.T) {
	const kept = int64(5368324170671202287)
	KeptIDs[kept] = true
	defer delete(KeptIDs, kept)
	dir := writeCollection(t)
	cat := &memCatalog{docs: map[[32]byte]int64{}}
	docs := &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}
	if _, err := (&Importer{Catalog: cat, Documents: docs}).Run(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if docs.byID[kept] == nil || docs.byID[5368324170671202286] != nil || len(docs.byID) != 2 {
		t.Fatalf("stored %v", docs.byID)
	}
	list := &mtproto.EmojiList{}
	if err := proto.Unmarshal(cat.stored.Answers[[2]string{"account.getDefaultProfilePhotoEmojis", ""}].Data, list); err != nil {
		t.Fatal(err)
	}
	if len(list.GetDocumentId()) != 1 || list.GetDocumentId()[0] != kept {
		t.Fatalf("emoji list %v", list.GetDocumentId())
	}
}

// A run that stored the documents and then failed leaves them referring to
// the ids the sets get in the next run (catalog_ids); and a document kept
// under Telegram's id that is stored already is not stored again.
func TestImportAfterAFailedRun(t *testing.T) {
	const kept = int64(5368324170671202287)
	KeptIDs[kept] = true
	defer delete(KeptIDs, kept)
	dir := writeCollection(t)
	cat := &memCatalog{docs: map[[32]byte]int64{}, failUpdate: true}
	docs := &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}
	im := &Importer{Catalog: cat, Documents: docs}
	if _, err := im.Run(context.Background(), dir); err == nil {
		t.Fatal("a failed store went unnoticed")
	}
	stored := len(docs.byID)
	// The kept document's record is lost, as by a crash before PutDocument.
	for k, v := range cat.docs {
		if v == kept {
			delete(cat.docs, k)
		}
	}
	cat.failUpdate = false
	res, err := im.Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewDocuments != 0 || len(docs.byID) != stored {
		t.Fatalf("stored again: %+v, %d documents then %d", res, stored, len(docs.byID))
	}
	set := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(cat.stored.Sets[0].Data, set); err != nil {
		t.Fatal(err)
	}
	for _, d := range set.GetDocuments() {
		if got := d.GetAttributes()[0].GetStickerset().GetId(); got != set.GetSet().GetId() {
			t.Fatalf("a sticker refers to set %d, the set is %d", got, set.GetSet().GetId())
		}
	}
}

// What only the collecting account had is not given to everyone.
func TestImportDropsAccountState(t *testing.T) {
	dir := writeCollection(t)
	rewriteManifest(t, dir, func(m *Manifest) {
		m.Answers = append(m.Answers, AnswerFile{Method: "messages.getFeaturedStickers", File: "answers/featured.json"})
	})
	b, _ := json.Marshal(map[string]any{
		"_": "messages.featuredStickers", "pFlags": map[string]any{}, "hash": 1, "count": 1,
		"sets": []any{map[string]any{"_": "stickerSetCovered",
			"set":   map[string]any{"_": "stickerSet", "pFlags": map[string]any{"archived": true}, "id": "777", "access_hash": "8", "title": "A", "short_name": "AnimatedEmojies", "count": 2, "hash": 1, "installed_date": 1700000000},
			"cover": map[string]any{"_": "documentEmpty", "id": "1"}}},
		"unread": []any{"777"},
	})
	if err := os.WriteFile(filepath.Join(dir, "answers/featured.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	cat := &memCatalog{docs: map[[32]byte]int64{}}
	if _, err := (&Importer{Catalog: cat, Documents: &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}}).Run(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	f := &mtproto.Messages_FeaturedStickers{}
	if err := proto.Unmarshal(cat.stored.Answers[[2]string{"messages.getFeaturedStickers", ""}].Data, f); err != nil {
		t.Fatal(err)
	}
	set := f.GetSets()[0].GetSet()
	if len(f.GetUnread()) != 0 || set.GetArchived() || set.GetInstalledDate() != nil {
		t.Fatalf("the collector's state: unread %v, set %v", f.GetUnread(), set)
	}
}

// A sticker of a set Telegram does not give out is kept, without its set.
func TestImportUnsharedSet(t *testing.T) {
	dir := writeCollection(t)
	rewriteManifest(t, dir, func(m *Manifest) { m.Sets = nil })
	cat := &memCatalog{docs: map[[32]byte]int64{}}
	docs := &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}
	res, err := (&Importer{Catalog: cat, Documents: docs}).Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.UnsharedSets == 0 || res.Sets != 0 || len(docs.byID) != 2 {
		t.Fatalf("result %+v, %d documents", res, len(docs.byID))
	}
	for _, d := range docs.byID {
		if p := d.GetAttributes()[0].GetStickerset().GetPredicateName(); p != mtproto.Predicate_inputStickerSetEmpty {
			t.Fatalf("a sticker of an unshared set refers to %s", p)
		}
	}
}

// Check finds what an import would stop on, and stores nothing.
func TestCheck(t *testing.T) {
	dir := writeCollection(t)
	r, err := Check(dir)
	if err != nil || r.Answers != 2 || r.Sets != 1 || r.Documents != 2 {
		t.Fatalf("check %+v, %v", r, err)
	}
	rewriteManifest(t, dir, func(m *Manifest) {
		m.Answers = append(m.Answers, AnswerFile{Method: "messages.getFeaturedStickers", File: "answers/featured.json"})
		m.Sets = nil
	})
	b, _ := json.Marshal(map[string]any{"_": "messages.featuredStickers", "pFlags": map[string]any{}, "hash": 1, "count": 1,
		"sets": []any{map[string]any{"_": "stickerSetCovered",
			"set":   map[string]any{"_": "stickerSet", "pFlags": map[string]any{}, "id": "777", "access_hash": "8", "title": "A", "short_name": "AnimatedEmojies", "count": 2, "hash": 1},
			"cover": map[string]any{"_": "documentEmpty", "id": "1"}}},
		"unread": []any{}})
	if err := os.WriteFile(filepath.Join(dir, "answers/featured.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(dir); err == nil {
		t.Fatal("a set the collection lacks passed the check")
	}
}

// A set fetched on demand: its documents with ids of this server, their
// files from the source, the set stored beside the catalog; again, nothing
// is fetched or stored twice.
func TestImportSet(t *testing.T) {
	sticker := func(id int64) *mtproto.Document {
		return mtproto.MakeTLDocument(&mtproto.Document{
			Id: id, AccessHash: 3, MimeType: "image/webp", DcId: 4, Size2_INT64: 4,
			Thumbs: []*mtproto.PhotoSize{mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "m", W: 128, H: 128, Size2: 2}).To_PhotoSize()},
			Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
				Alt: "😀", Stickerset: mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 55, AccessHash: 1}).To_InputStickerSet(),
			}).To_DocumentAttribute()},
		}).To_Document()
	}
	newSet := func() *mtproto.Messages_StickerSet {
		return mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
			Set:       mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 55, AccessHash: 1, ShortName: "Free420", Title: "Free", Count: 2}).To_StickerSet(),
			Packs:     []*mtproto.StickerPack{mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "😀", Documents: []int64{8001, 8002}}).To_StickerPack()},
			Documents: []*mtproto.Document{sticker(8001), sticker(8002)},
		}).To_Messages_StickerSet()
	}
	fetched := 0
	fetch := func(_ context.Context, doc *mtproto.Document, thumb string) ([]byte, error) {
		fetched++
		if thumb != "" {
			return []byte("mm"), nil
		}
		return []byte(fmt.Sprintf("%04d", doc.GetId()%10000)), nil
	}
	cat := &memCatalog{docs: map[[32]byte]int64{}}
	docs := &memDocuments{byID: map[int64]*mtproto.Document{}, files: map[int64][]byte{}}
	im := &Importer{Catalog: cat, Documents: docs}
	stored, err := im.ImportSet(context.Background(), newSet(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 4 || len(docs.byID) != 2 || cat.demand["Free420"] != stored || stored.ID == 55 {
		t.Fatalf("fetched %d, stored %d documents, set %+v", fetched, len(docs.byID), stored)
	}
	set := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(stored.Data, set); err != nil {
		t.Fatal(err)
	}
	for _, d := range set.GetDocuments() {
		if d.GetId() < 1001 || d.GetAttributes()[0].GetStickerset().GetId() != stored.ID {
			t.Fatalf("document %v", d)
		}
	}
	if set.GetPacks()[0].GetDocuments()[0] < 1001 || set.GetSet().GetHash() != stored.Hash {
		t.Fatalf("set %v", set)
	}
	fetched = 0
	again, err := im.ImportSet(context.Background(), newSet(), fetch)
	if err != nil || again.ID != stored.ID || len(docs.byID) != 2 {
		t.Fatalf("again: %+v, %v, %d documents", again, err, len(docs.byID))
	}
}
