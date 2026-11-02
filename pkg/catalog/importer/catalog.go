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
	"bytes"
	"fmt"
	"io"
	"strconv"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// The methods whose hash is an int (layer 228); the others' is a long.
var intHash = map[string]bool{
	"messages.getAvailableReactions":      true,
	"messages.getAvailableEffects":        true,
	"messages.getEmojiGroups":             true,
	"messages.getEmojiStatusGroups":       true,
	"messages.getEmojiProfilePhotoGroups": true,
	"messages.getEmojiStickerGroups":      true,
	"help.getPeerColors":                  true,
	"help.getPeerProfileColors":           true,
	"help.getTimezonesList":               true,
}

// catalog builds the catalog to store from a renumbered collection: the
// sets with their hashes, every stickerSet the answers hold with the hash
// of its set, then the answers with theirs, and every wallpaper by slug
// and by id.
func (im *Importer) catalog(c *collection) (*catalog.Catalog, error) {
	out := &catalog.Catalog{Answers: map[[2]string]catalog.Answer{}}
	if im.Now != nil {
		out.Now = im.Now()
	}

	// What only the collecting account had goes: its unread trending sets,
	// when it installed or archived a set, the wallpapers it made. Every
	// account gets the catalog as one that did none of it.
	//
	// A set's own thumbnail (inputStickerSetThumb) goes too: the server has
	// no plugin for it (app/bff/files, Plugin1), and a client shown one it
	// cannot fetch draws an empty cover. Without one, clients cover a set
	// with its thumb_document_id or its first sticker.
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			switch v := x.Interface().(type) {
			case *mtproto.StickerSet:
				v.Thumbs, v.ThumbDcId, v.ThumbVersion = nil, nil, nil
				v.InstalledDate, v.Archived = nil, false
			case *mtproto.Messages_FeaturedStickers:
				v.Unread = []int64{}
			case *mtproto.WallPaper:
				v.Creator = false
			}
		})
	}

	setHash := map[int64]int32{}
	for i, s := range c.sets {
		s.Set.Hash = 0
		h := int32(digestHash(s))
		s.Set.Hash = h
		setHash[s.GetSet().GetId()] = h
		data, err := proto.Marshal(s)
		if err != nil {
			return nil, err
		}
		out.Sets = append(out.Sets, &catalog.Set{
			ID: s.GetSet().GetId(), AccessHash: s.GetSet().GetAccessHash(), ShortName: s.GetSet().GetShortName(),
			Kind: c.setKinds[i], Hash: h, Data: data,
		})
	}
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if set, ok := x.Interface().(*mtproto.StickerSet); ok {
				if h, ok := setHash[set.GetId()]; ok {
					set.Hash = h
				}
			}
		})
	}

	for _, key := range c.answerKeys() {
		m := c.answers[key]
		h, err := answerHash(key[0], m)
		if err != nil {
			return nil, err
		}
		data, err := proto.Marshal(m)
		if err != nil {
			return nil, err
		}
		out.Answers[key] = catalog.Answer{Hash: h, Data: data}
	}

	// Every wallpaper of the collection by id and slug: those fetched by
	// slug, the gallery's and the themes' (Android fetches a cloud theme's
	// wallpaper by its slug, Theme.java).
	var wallpapers []*mtproto.WallPaper
	for _, m := range c.all() {
		walkMessages(m.ProtoReflect(), func(x protoreflect.Message) {
			if w, ok := x.Interface().(*mtproto.WallPaper); ok {
				wallpapers = append(wallpapers, w)
			}
		})
	}
	for _, w := range wallpapers {
		data, err := proto.Marshal(w)
		if err != nil {
			return nil, err
		}
		out.Answers[[2]string{"account.getWallPaper", "id:" + strconv.FormatInt(w.GetId(), 10)}] = catalog.Answer{Data: data}
		if w.GetSlug() != "" {
			out.Answers[[2]string{"account.getWallPaper", "slug:" + w.GetSlug()}] = catalog.Answer{Data: data}
		}
	}
	return out, nil
}

// answerHash counts the hash of an answer and puts it in its hash field.
func answerHash(method string, m proto.Message) (int64, error) {
	r := m.ProtoReflect()
	fd := r.Descriptor().Fields().ByName("hash")
	if fd == nil {
		return 0, nil // emoji keywords: versions, no hash
	}
	if fd.Kind() != protoreflect.Int64Kind && fd.Kind() != protoreflect.Int32Kind {
		return 0, fmt.Errorf("importer: %s: a hash of kind %s", method, fd.Kind())
	}
	r.Clear(fd)
	var h int64
	switch x := m.(type) {
	case *mtproto.Messages_FeaturedStickers:
		h = featuredHash(x)
	case *mtproto.Account_WallPapers:
		h = wallPapersHash(x)
	case *mtproto.Messages_Reactions:
		h = reactionsHash(x)
	default:
		h = digestHash(m)
	}
	if intHash[method] {
		h = int32Hash(h)
	}
	if fd.Kind() == protoreflect.Int32Kind {
		r.Set(fd, protoreflect.ValueOfInt32(int32(h)))
	} else {
		r.Set(fd, protoreflect.ValueOfInt64(h))
	}
	return h, nil
}
