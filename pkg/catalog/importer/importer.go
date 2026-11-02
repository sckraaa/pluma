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
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/tl"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Documents stores the documents of a catalog (dfs.importDocument and
// media.importDocument, RPCDocuments) and reads stored ones.
type Documents interface {
	// Import stores a document as it is: its file, its downloadable
	// thumbnails by type, and the metadata of doc (ids aside), under id if
	// not 0, else under a new id.
	Import(ctx context.Context, doc *mtproto.Document, file []byte, thumbs map[string][]byte, id int64) (*mtproto.Document, error)
	// Get returns the stored documents of ids.
	Get(ctx context.Context, ids []int64) ([]*mtproto.Document, error)
}

// Catalog is the store of the catalog (catalog.Store).
type Catalog interface {
	IDFor(ctx context.Context, kind, name string, newID func() (int64, int64, error)) (int64, int64, error)
	DocumentID(ctx context.Context, key [32]byte) (int64, error)
	PutDocument(ctx context.Context, key [32]byte, documentID int64) error
	Replace(ctx context.Context, c *catalog.Catalog) error
}

// Importer puts collections into the server.
type Importer struct {
	Catalog   Catalog
	Documents Documents
	Now       func() int64
	Logf      func(format string, args ...any)
}

// Result counts what an import did.
type Result struct {
	Answers, Sets, Documents, NewDocuments int
	// UnsharedSets counts the references to sets Telegram does not give
	// out, made inputStickerSetEmpty (unshared).
	UnsharedSets int
}

// collection is a collection read and decoded.
type collection struct {
	root       *os.Root
	manifest   *Manifest
	answers    map[[2]string]proto.Message
	sets       []*mtproto.Messages_StickerSet
	setKinds   []string
	extra      []*mtproto.Document
	wallpapers []*mtproto.WallPaper
}

// Check reads, decodes and renumbers the collection at dir as Run does,
// with ids made up in memory, and stores nothing: a reference to anything
// the collection lacks, or an object it cannot read, is the error an
// import would stop on. It does not read the documents' files.
func Check(dir string) (*Result, error) {
	c, err := read(dir)
	if err != nil {
		return nil, err
	}
	defer c.root.Close()
	unshared := c.unshared()
	ctx := context.Background()
	im := &Importer{Catalog: &dryCatalog{}}
	ids := NewIDs()
	if err := im.setIDs(ctx, c, ids); err != nil {
		return nil, err
	}
	if err := im.otherIDs(ctx, c, ids); err != nil {
		return nil, err
	}
	next := int64(0)
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if d, ok := x.Interface().(*mtproto.Document); ok && d.GetPredicateName() == mtproto.Predicate_document && ids.Documents[d.GetId()] == nil {
				next++
				stored := proto.Clone(d).(*mtproto.Document)
				stored.Id = next
				ids.Documents[d.GetId()] = stored
			}
		})
	}
	for _, m := range c.all() {
		if err := ids.Rewrite(m); err != nil {
			return nil, err
		}
	}
	return &Result{Answers: len(c.answers), Sets: len(c.sets), Documents: len(ids.Documents), UnsharedSets: unshared}, nil
}

// dryCatalog gives ids in memory for Check.
type dryCatalog struct{ Catalog }

func (dryCatalog) IDFor(_ context.Context, _, _ string, newID func() (int64, int64, error)) (int64, int64, error) {
	return newID()
}

// Run imports the collection at dir: everything it holds replaces the
// catalog, or, on any error, the catalog stays as it was (documents
// stored on the way are kept for the next run).
func (im *Importer) Run(ctx context.Context, dir string) (*Result, error) {
	c, err := read(dir)
	if err != nil {
		return nil, err
	}
	defer c.root.Close()

	unshared := c.unshared()
	ids := NewIDs()
	if err := im.setIDs(ctx, c, ids); err != nil {
		return nil, err
	}
	if err := im.otherIDs(ctx, c, ids); err != nil {
		return nil, err
	}
	stored, err := im.documents(ctx, c, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range c.all() {
		if err := ids.Rewrite(m); err != nil {
			return nil, err
		}
	}
	out, err := im.catalog(c)
	if err != nil {
		return nil, err
	}
	if err := im.Catalog.Replace(ctx, out); err != nil {
		return nil, err
	}
	return &Result{Answers: len(out.Answers), Sets: len(out.Sets), Documents: len(ids.Documents), NewDocuments: stored, UnsharedSets: unshared}, nil
}

func read(dir string) (*collection, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	m, err := readManifest(root, tl.Layer)
	if err != nil {
		root.Close()
		return nil, err
	}
	c := &collection{root: root, manifest: m, answers: map[[2]string]proto.Message{}}
	fail := func(err error) (*collection, error) {
		root.Close()
		return nil, err
	}
	for _, a := range m.Answers {
		o, err := c.object(a.File)
		if err != nil {
			return fail(err)
		}
		key := [2]string{a.Method, a.Arg}
		if c.answers[key] != nil {
			return fail(fmt.Errorf("importer: %s(%q) twice", a.Method, a.Arg))
		}
		c.answers[key] = o
	}
	for _, s := range m.Sets {
		o, err := c.object(s.File)
		if err != nil {
			return fail(err)
		}
		set, ok := o.(*mtproto.Messages_StickerSet)
		if !ok || set.GetPredicateName() != mtproto.Predicate_messages_stickerSet {
			return fail(fmt.Errorf("importer: %s: not a messages.stickerSet", s.File))
		}
		c.sets = append(c.sets, set)
		c.setKinds = append(c.setKinds, s.Kind)
	}
	for _, f := range m.ExtraDocuments {
		o, err := c.object(f)
		if err != nil {
			return fail(err)
		}
		d, ok := o.(*mtproto.Document)
		if !ok || d.GetPredicateName() != mtproto.Predicate_document {
			return fail(fmt.Errorf("importer: %s: not a document", f))
		}
		c.extra = append(c.extra, d)
	}
	for _, f := range m.WallPapers {
		o, err := c.object(f)
		if err != nil {
			return fail(err)
		}
		w, ok := o.(*mtproto.WallPaper)
		if !ok {
			return fail(fmt.Errorf("importer: %s: not a wallpaper", f))
		}
		c.wallpapers = append(c.wallpapers, w)
	}
	return c, nil
}

// object reads the tweb object in file and decodes it into the server's
// class (mtproto.Document, mtproto.Messages_StickerSet…).
func (c *collection) object(file string) (proto.Message, error) {
	b, err := c.root.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var v any
	d := json.NewDecoder(bytesReader(b))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("importer: %s: %w", file, err)
	}
	o, err := tl.Default.Decode(v)
	if err != nil {
		return nil, fmt.Errorf("importer: %s: %w", file, err)
	}
	return classOf(o)
}

// classOf is the class a decoded TL object wraps (its Data2).
func classOf(o mtproto.TLObject) (proto.Message, error) {
	v := reflect.ValueOf(o)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	f := v.FieldByName("Data2")
	if !f.IsValid() {
		return nil, fmt.Errorf("importer: %T is no TL constructor", o)
	}
	m, ok := f.Interface().(proto.Message)
	if !ok || f.IsNil() {
		return nil, fmt.Errorf("importer: %T has no class", o)
	}
	return m, nil
}

// unshared turns the references of documents to sets the collection does
// not hold into inputStickerSetEmpty, and counts them; it drops the covers
// of sets named by a document the collection lacks. Telegram names such
// sets in documents it gives out, then answers messages.getStickerSet for
// them with STICKERSET_INVALID (the custom emoji of
// account.getDefaultProfilePhotoEmojis, 2026-11-02); the collector asked
// for each, so a set it lacks is one Telegram does not give. The document
// stays, without a set to open; no id of Telegram is kept for the set.
func (c *collection) unshared() int {
	held := map[int64]bool{}
	for _, s := range c.sets {
		held[s.GetSet().GetId()] = true
	}
	// A set's cover named by id is a document the collection may lack (a
	// custom emoji of another set): without it, clients cover the set with
	// its first sticker.
	docs := map[int64]bool{}
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if d, ok := x.Interface().(*mtproto.Document); ok && d.GetPredicateName() == mtproto.Predicate_document {
				docs[d.GetId()] = true
			}
		})
	}
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if set, ok := x.Interface().(*mtproto.StickerSet); ok && set.GetThumbDocumentId() != nil && !docs[set.GetThumbDocumentId().GetValue()] {
				set.ThumbDocumentId = nil
			}
		})
	}
	n := 0
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			a, ok := x.Interface().(*mtproto.DocumentAttribute)
			if !ok {
				return
			}
			switch a.GetPredicateName() {
			case mtproto.Predicate_documentAttributeSticker, mtproto.Predicate_documentAttributeCustomEmoji:
			default:
				return
			}
			if ref := a.GetStickerset(); ref.GetPredicateName() == mtproto.Predicate_inputStickerSetID && !held[ref.GetId()] {
				a.Stickerset = mtproto.MakeTLInputStickerSetEmpty(nil).To_InputStickerSet()
				n++
			}
		})
	}
	return n
}

// all is every object of the collection.
func (c *collection) all() []proto.Message {
	var out []proto.Message
	for _, key := range c.answerKeys() {
		out = append(out, c.answers[key])
	}
	for _, s := range c.sets {
		out = append(out, s)
	}
	for _, d := range c.extra {
		out = append(out, d)
	}
	for _, w := range c.wallpapers {
		out = append(out, w)
	}
	return out
}

func (c *collection) answerKeys() [][2]string {
	keys := make([][2]string, 0, len(c.answers))
	for k := range c.answers {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}

// setIDs gives every set of the collection the id of this server it has
// by its short name (catalog_ids), or a new one kept from now on.
func (im *Importer) setIDs(ctx context.Context, c *collection, ids *IDs) error {
	names := map[string]bool{}
	for _, s := range c.sets {
		set := s.GetSet()
		name := strings.ToLower(set.GetShortName())
		if name == "" || names[name] {
			return fmt.Errorf("importer: set %q without a name or twice", set.GetShortName())
		}
		names[name] = true
		id, hash, err := im.Catalog.IDFor(ctx, "set", name, newIDPair)
		if err != nil {
			return err
		}
		ids.Sets[set.GetId()] = idHash{ID: id, AccessHash: hash}
	}
	return nil
}

// otherIDs gives wallpapers and themes the ids of this server they have by
// their slug, or, without one, by what they are (catalog_ids).
func (im *Importer) otherIDs(ctx context.Context, c *collection, ids *IDs) error {
	var err error
	give := func(kind, name string, to map[int64]idHash, tgID int64) {
		if _, ok := to[tgID]; ok || err != nil {
			return
		}
		var id, hash int64
		if id, hash, err = im.Catalog.IDFor(ctx, kind, name, newIDPair); err == nil {
			to[tgID] = idHash{ID: id, AccessHash: hash}
		}
	}
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			switch v := x.Interface().(type) {
			case *mtproto.WallPaper:
				give("wallpaper", nameOf(v.GetSlug(), v), ids.WallPapers, v.GetId())
			case *mtproto.Theme:
				give("theme", nameOf(v.GetSlug(), v), ids.Themes, v.GetId())
			}
		})
	}
	return err
}

// nameOf is what names a wallpaper or a theme: its slug, or a digest of it
// with its ids left out (a wallPaperNoFile has no slug).
func nameOf(slug string, m proto.Message) string {
	if slug != "" {
		return "slug:" + slug
	}
	c := proto.Clone(m)
	r := c.ProtoReflect()
	for _, f := range []protoreflect.Name{"id", "access_hash"} {
		if fd := r.Descriptor().Fields().ByName(f); fd != nil {
			r.Clear(fd)
		}
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(c)
	if err != nil {
		panic(err) // a message that was just decoded always encodes
	}
	sum := sha256.Sum256(b)
	return "digest:" + hex.EncodeToString(sum[:16])
}

func newIDPair() (int64, int64, error) {
	n, err := newIDHash()
	return n.ID, n.AccessHash, err
}

// documents stores the documents of the collection, or finds those stored
// by an earlier import, and returns how many it stored.
func (im *Importer) documents(ctx context.Context, c *collection, ids *IDs) (int, error) {
	files := map[int64]DocumentFiles{}
	for _, f := range c.manifest.Files {
		id, err := strconv.ParseInt(f.ID, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("importer: files of %q: %w", f.ID, err)
		}
		files[id] = f
	}
	setNames := map[int64]string{}
	for _, s := range c.sets {
		setNames[s.GetSet().GetId()] = s.GetSet().GetShortName()
	}

	docs := map[int64]*mtproto.Document{}
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if d, ok := x.Interface().(*mtproto.Document); ok && d.GetPredicateName() == mtproto.Predicate_document && docs[d.GetId()] == nil {
				docs[d.GetId()] = proto.Clone(d).(*mtproto.Document)
			}
		})
	}
	tgIDs := make([]int64, 0, len(docs))
	for id := range docs {
		tgIDs = append(tgIDs, id)
	}
	sort.Slice(tgIDs, func(i, j int) bool { return tgIDs[i] < tgIDs[j] })

	stored := 0
	known := map[int64]int64{} // Telegram's id → this server's, stored earlier
	for _, tgID := range tgIDs {
		doc := docs[tgID]
		f, ok := files[tgID]
		if !ok {
			return 0, fmt.Errorf("importer: document %d has no files", tgID)
		}
		file, thumbs, err := c.files(doc, f)
		if err != nil {
			return 0, err
		}
		key := documentKey(file, doc, setNames[setOf(doc)])
		id, err := im.Catalog.DocumentID(ctx, key)
		if err != nil {
			return 0, err
		}
		if id != 0 {
			known[tgID] = id
			continue
		}
		// Its attributes refer to its set by this server's id.
		meta := proto.Clone(doc).(*mtproto.Document)
		for _, a := range meta.GetAttributes() {
			if err := ids.Rewrite(a); err != nil {
				return 0, err
			}
		}
		keep := int64(0)
		if KeptIDs[tgID] {
			keep = tgID
			// Stored under its id by a run that stopped before recording
			// it: that one, never its files written again.
			got, err := im.Documents.Get(ctx, []int64{tgID})
			if err != nil {
				return 0, err
			}
			if len(got) > 0 && got[0].GetPredicateName() == mtproto.Predicate_document {
				if err := im.Catalog.PutDocument(ctx, key, tgID); err != nil {
					return 0, err
				}
				known[tgID] = tgID
				continue
			}
		}
		d, err := im.Documents.Import(ctx, meta, file, thumbs, keep)
		if err != nil {
			return 0, fmt.Errorf("importer: document %d: %w", tgID, err)
		}
		if err := im.Catalog.PutDocument(ctx, key, d.GetId()); err != nil {
			return 0, err
		}
		ids.Documents[tgID] = d
		stored++
		if im.Logf != nil && stored%500 == 0 {
			im.Logf("importer: %d documents stored", stored)
		}
	}
	if len(known) > 0 {
		list := make([]int64, 0, len(known))
		for _, id := range known {
			list = append(list, id)
		}
		got, err := im.Documents.Get(ctx, list)
		if err != nil {
			return 0, err
		}
		byID := map[int64]*mtproto.Document{}
		for _, d := range got {
			if d.GetPredicateName() == mtproto.Predicate_document {
				byID[d.GetId()] = d
			}
		}
		for tgID, id := range known {
			if byID[id] == nil {
				return 0, fmt.Errorf("importer: document %d was stored as %d, which is gone", tgID, id)
			}
			ids.Documents[tgID] = byID[id]
		}
	}
	return stored, nil
}

// KeptIDs are the documents that keep Telegram's id, because clients name
// them by it in their code (owner's decision, ADR 0028;
// docs/tasks/024-research/semantics.md):
//   - tweb: the icon of the premium emoji group (appEmojiManager.ts) and
//     the icon of a General topic (messageActionTextNewUnsafe.ts);
//   - Android: DIAMOND_EMOJI (ChannelMonetizationLayout.java), the custom
//     emoji of the theme preview (ThemePreviewMessagesCell.java) and of
//     TranslateButton.
//
// The collector asks for them by id (tools/catalog-collector).
var KeptIDs = map[int64]bool{
	5269590556232664327: true,
	5390854796011906616: true,
	5471952986970267163: true,
	5386654653003864312: true,
	5373141891321699086: true,
	5197252827247841976: true,
}

// files reads the file of doc and of its downloadable thumbnails; a
// thumbnail without its file is an error.
func (c *collection) files(doc *mtproto.Document, f DocumentFiles) ([]byte, map[string][]byte, error) {
	file, err := c.root.ReadFile(f.File)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(file)) != doc.GetSize2_INT64() && doc.GetSize2_INT64() != 0 {
		return nil, nil, fmt.Errorf("importer: document %d: %d bytes, %d expected", doc.GetId(), len(file), doc.GetSize2_INT64())
	}
	thumbs := map[string][]byte{}
	want := func(typ string) error {
		name, ok := f.Thumbs[typ]
		if !ok {
			return fmt.Errorf("importer: document %d: no file of thumbnail %q", doc.GetId(), typ)
		}
		b, err := c.root.ReadFile(name)
		if err != nil {
			return err
		}
		thumbs[typ] = b
		return nil
	}
	for _, t := range doc.GetThumbs() {
		switch t.GetPredicateName() {
		case mtproto.Predicate_photoSize, mtproto.Predicate_photoSizeProgressive:
			if err := want(t.GetType()); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, v := range doc.GetVideoThumbs() {
		if v.GetPredicateName() == mtproto.Predicate_videoSize {
			if err := want(v.GetType()); err != nil {
				return nil, nil, err
			}
		}
	}
	return file, thumbs, nil
}

// setOf is the set a sticker or custom emoji belongs to, 0 for none.
func setOf(doc *mtproto.Document) int64 {
	for _, a := range doc.GetAttributes() {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeSticker, mtproto.Predicate_documentAttributeCustomEmoji:
			if a.GetStickerset().GetPredicateName() == mtproto.Predicate_inputStickerSetID {
				return a.GetStickerset().GetId()
			}
		}
	}
	return 0
}

// documentKey is what makes a document that document: its file, what it
// is (type, emoji, file name) and the set it belongs to; one file can be
// in two sets, or twice in one as two stickers.
func documentKey(file []byte, doc *mtproto.Document, set string) [32]byte {
	h := sha256.New()
	sum := sha256.Sum256(file)
	h.Write(sum[:])
	for _, part := range []string{doc.GetMimeType(), set, altOf(doc), fileNameOf(doc)} {
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

func altOf(doc *mtproto.Document) string {
	for _, a := range doc.GetAttributes() {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeSticker, mtproto.Predicate_documentAttributeCustomEmoji:
			return a.GetAlt()
		}
	}
	return ""
}

func fileNameOf(doc *mtproto.Document) string {
	for _, a := range doc.GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeFilename {
			return a.GetFileName()
		}
	}
	return ""
}

// newIDHash is a new positive id and access hash.
func newIDHash() (idHash, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return idHash{}, err
	}
	return idHash{
		ID:         int64(binary.LittleEndian.Uint64(b[:8])>>1) | 1,
		AccessHash: int64(binary.LittleEndian.Uint64(b[8:])),
	}, nil
}

// walkMessages calls f for m and every message in it.
func walkMessages(m protoreflect.Message, f func(protoreflect.Message)) {
	f(m)
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() == nil || fd.IsMap() {
			return true
		}
		if fd.IsList() {
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				walkMessages(list.Get(i).Message(), f)
			}
		} else {
			walkMessages(v.Message(), f)
		}
		return true
	})
}
