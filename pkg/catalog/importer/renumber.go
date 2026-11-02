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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// idHash is an id of this server and its access hash.
type idHash struct {
	ID         int64
	AccessHash int64
}

// IDs maps the ids of Telegram in a catalog to the ids of this server
// (ADR 0028: no id of Telegram is kept). Every reference the catalog makes
// must be in it: a reference to something not imported is an error, never
// an id left as Telegram's.
type IDs struct {
	Documents  map[int64]*mtproto.Document // by Telegram's id, the stored document
	Sets       map[int64]idHash
	WallPapers map[int64]idHash
	Themes     map[int64]idHash
}

// NewIDs returns an empty map.
func NewIDs() *IDs {
	return &IDs{
		Documents:  map[int64]*mtproto.Document{},
		Sets:       map[int64]idHash{},
		WallPapers: map[int64]idHash{},
		Themes:     map[int64]idHash{},
	}
}

// What a raw id field holds, by message and field (layer 228): documents
// referred to by id, and sets.
const (
	refDocument = iota + 1
	refSet
)

var idFields = map[protoreflect.FullName]map[protoreflect.Name]int{
	"mtproto.StickerPack":               {"documents": refDocument},
	"mtproto.EmojiList":                 {"document_id": refDocument},
	"mtproto.EmojiStatus":               {"document_id": refDocument, "pattern_document_id": refDocument},
	"mtproto.AvailableEffect":           {"static_icon_id": refDocument, "effect_sticker_id": refDocument, "effect_animation_id": refDocument},
	"mtproto.Reaction":                  {"document_id": refDocument},
	"mtproto.EmojiGroup":                {"icon_emoji_id": refDocument, "document_id": refDocument},
	"mtproto.StickerKeyword":            {"document_id": refDocument},
	"mtproto.Messages_FeaturedStickers": {"unread": refSet},
	"mtproto.StickerSet":                {"thumb_document_id": refDocument},
}

// Rewrite puts the ids of this server in msg, in place: documents whole,
// references to sets, wallpapers, themes and documents by id.
func (ids *IDs) Rewrite(msg proto.Message) error {
	return ids.walk(msg.ProtoReflect(), string(msg.ProtoReflect().Descriptor().FullName()))
}

func (ids *IDs) walk(m protoreflect.Message, path string) error {
	name := m.Descriptor().FullName()
	if err := ids.own(m, path); err != nil {
		return err
	}
	var err error
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		p := path + "." + string(fd.Name())
		switch {
		case fd.Message() != nil && fd.IsList():
			list := v.List()
			for i := 0; i < list.Len() && err == nil; i++ {
				err = ids.message(list.Get(i).Message(), fmt.Sprintf("%s[%d]", p, i), func(n protoreflect.Message) { list.Set(i, protoreflect.ValueOfMessage(n)) })
			}
		case fd.Message() != nil && !fd.IsMap() && idFields[name][fd.Name()] != 0:
			// An optional id: a google.protobuf.Int64Value.
			w, ok := v.Message().Interface().(*wrapperspb.Int64Value)
			if !ok {
				err = fmt.Errorf("importer: %s: an id of type %s", p, fd.Message().FullName())
				break
			}
			var n int64
			if n, err = ids.ref(idFields[name][fd.Name()], w.GetValue(), p); err == nil {
				m.Set(fd, protoreflect.ValueOfMessage(wrapperspb.Int64(n).ProtoReflect()))
			}
		case fd.Message() != nil && !fd.IsMap():
			err = ids.message(v.Message(), p, func(n protoreflect.Message) { m.Set(fd, protoreflect.ValueOfMessage(n)) })
		case fd.Kind() == protoreflect.Int64Kind:
			ref := idFields[name][fd.Name()]
			if ref == 0 {
				return true
			}
			if fd.IsList() {
				list := v.List()
				for i := 0; i < list.Len() && err == nil; i++ {
					var n int64
					if n, err = ids.ref(ref, list.Get(i).Int(), p); err == nil {
						list.Set(i, protoreflect.ValueOfInt64(n))
					}
				}
			} else if v.Int() != 0 {
				var n int64
				if n, err = ids.ref(ref, v.Int(), p); err == nil {
					m.Set(fd, protoreflect.ValueOfInt64(n))
				}
			}
		}
		return err == nil
	})
	return err
}

// message rewrites a message field: a document is replaced by the stored
// one, anything else is walked.
func (ids *IDs) message(m protoreflect.Message, path string, replace func(protoreflect.Message)) error {
	if doc, ok := m.Interface().(*mtproto.Document); ok && doc.GetPredicateName() == mtproto.Predicate_document {
		stored := ids.Documents[doc.GetId()]
		if stored == nil {
			return fmt.Errorf("importer: %s: document %d not imported", path, doc.GetId())
		}
		replace(proto.Clone(stored).ProtoReflect())
		return nil
	}
	return ids.walk(m, path)
}

// own rewrites the ids a message carries as its own: a set, a reference to
// a set, a wallpaper, a theme.
func (ids *IDs) own(m protoreflect.Message, path string) error {
	switch x := m.Interface().(type) {
	case *mtproto.StickerSet:
		n, ok := ids.Sets[x.GetId()]
		if !ok {
			return fmt.Errorf("importer: %s: set %d not imported", path, x.GetId())
		}
		x.Id, x.AccessHash = n.ID, n.AccessHash
	case *mtproto.InputStickerSet:
		if x.GetPredicateName() == mtproto.Predicate_inputStickerSetID {
			n, ok := ids.Sets[x.GetId()]
			if !ok {
				return fmt.Errorf("importer: %s: set %d not imported", path, x.GetId())
			}
			x.Id, x.AccessHash = n.ID, n.AccessHash
		}
	case *mtproto.WallPaper:
		n, ok := ids.WallPapers[x.GetId()]
		if !ok {
			return fmt.Errorf("importer: %s: wallpaper %d not imported", path, x.GetId())
		}
		x.Id, x.AccessHash = n.ID, n.AccessHash
	case *mtproto.Theme:
		n, ok := ids.Themes[x.GetId()]
		if !ok {
			return fmt.Errorf("importer: %s: theme %d not imported", path, x.GetId())
		}
		x.Id, x.AccessHash = n.ID, n.AccessHash
	}
	return nil
}

func (ids *IDs) ref(kind int, id int64, path string) (int64, error) {
	switch kind {
	case refDocument:
		if d := ids.Documents[id]; d != nil {
			return d.GetId(), nil
		}
		return 0, fmt.Errorf("importer: %s: document %d not imported", path, id)
	case refSet:
		if n, ok := ids.Sets[id]; ok {
			return n.ID, nil
		}
		return 0, fmt.Errorf("importer: %s: set %d not imported", path, id)
	}
	return 0, fmt.Errorf("importer: %s: unknown reference", path)
}
