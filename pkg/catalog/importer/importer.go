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
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"

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
	// thumbnails by type, and the metadata of doc (ids aside).
	Import(ctx context.Context, doc *mtproto.Document, file []byte, thumbs map[string][]byte) (*mtproto.Document, error)
	// Get returns the stored documents of ids.
	Get(ctx context.Context, ids []int64) ([]*mtproto.Document, error)
}

// Catalog is the store of the catalog (catalog.Store).
type Catalog interface {
	Answer(ctx context.Context, method, arg string) (*catalog.Answer, error)
	SetByShortName(ctx context.Context, name string) (*catalog.Set, error)
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

// Run imports the collection at dir: everything it holds replaces the
// catalog, or, on any error, the catalog stays as it was (documents
// stored on the way are kept for the next run).
func (im *Importer) Run(ctx context.Context, dir string) (*Result, error) {
	c, err := read(dir)
	if err != nil {
		return nil, err
	}
	defer c.root.Close()

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
	return &Result{Answers: len(out.Answers), Sets: len(out.Sets), Documents: len(ids.Documents), NewDocuments: stored}, nil
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

// setIDs gives every set of the collection an id of this server: the one
// it has by short name, or a new one.
func (im *Importer) setIDs(ctx context.Context, c *collection, ids *IDs) error {
	names := map[string]bool{}
	for _, s := range c.sets {
		set := s.GetSet()
		if names[set.GetShortName()] {
			return fmt.Errorf("importer: set %q twice", set.GetShortName())
		}
		names[set.GetShortName()] = true
		old, err := im.Catalog.SetByShortName(ctx, set.GetShortName())
		if err != nil {
			return err
		}
		if old != nil {
			ids.Sets[set.GetId()] = idHash{ID: old.ID, AccessHash: old.AccessHash}
			continue
		}
		n, err := newIDHash()
		if err != nil {
			return err
		}
		ids.Sets[set.GetId()] = n
	}
	return nil
}

// otherIDs gives wallpapers and themes ids of this server: a wallpaper the
// one it has by slug, anything else a new one.
func (im *Importer) otherIDs(ctx context.Context, c *collection, ids *IDs) error {
	var err error
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if err != nil {
				return
			}
			switch v := x.Interface().(type) {
			case *mtproto.WallPaper:
				if _, ok := ids.WallPapers[v.GetId()]; ok {
					return
				}
				var n idHash
				if n, err = im.wallPaperID(ctx, v); err == nil {
					ids.WallPapers[v.GetId()] = n
				}
			case *mtproto.Theme:
				if _, ok := ids.Themes[v.GetId()]; ok {
					return
				}
				var n idHash
				if n, err = newIDHash(); err == nil {
					ids.Themes[v.GetId()] = n
				}
			}
		})
	}
	return err
}

func (im *Importer) wallPaperID(ctx context.Context, w *mtproto.WallPaper) (idHash, error) {
	if w.GetSlug() != "" {
		a, err := im.Catalog.Answer(ctx, "account.getWallPaper", "slug:"+w.GetSlug())
		if err != nil {
			return idHash{}, err
		}
		if a != nil {
			old := &mtproto.WallPaper{}
			if err := proto.Unmarshal(a.Data, old); err != nil {
				return idHash{}, err
			}
			return idHash{ID: old.GetId(), AccessHash: old.GetAccessHash()}, nil
		}
	}
	return newIDHash()
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
		key := documentKey(file, setNames[setOf(doc)])
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
		d, err := im.Documents.Import(ctx, meta, file, thumbs)
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

// documentKey is what makes a document that document: its file and the
// set it belongs to (one file can be in two sets).
func documentKey(file []byte, set string) [32]byte {
	h := sha256.New()
	sum := sha256.Sum256(file)
	h.Write(sum[:])
	h.Write([]byte{0})
	h.Write([]byte(set))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
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
