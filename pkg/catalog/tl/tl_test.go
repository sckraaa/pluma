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

package tl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A sticker as tweb returns it from messages.getStickerSet: a long id over
// 2^53 as a string, bytes as $b64 or as an array of numbers (as tweb
// keeps file_reference and thumbnail bytes, seen 2026-11-02 in
// messages.getAvailableReactions), an inline path thumbnail, a set
// reference in its attribute, an empty one, and a true flag in pFlags.
const sticker = `{
  "_": "document",
  "pFlags": {},
  "id": "5368324170671202286",
  "access_hash": 1234567,
  "file_reference": [1, 2, 3],
  "date": 1700000000,
  "mime_type": "application/x-tgsticker",
  "size": 31337,
  "thumbs": [
    {"_": "photoPathSize", "type": "j", "bytes": [4, 5, 6]},
    {"_": "photoSize", "type": "m", "w": 128, "h": 128, "size": 4096}
  ],
  "video_thumbs": [{"_": "videoSize", "pFlags": {}, "type": "f", "w": 100, "h": 100, "size": 999}],
  "dc_id": 2,
  "attributes": [
    {"_": "documentAttributeImageSize", "w": 512, "h": 512},
    {"_": "documentAttributeSticker", "pFlags": {"mask": true}, "alt": "😀",
     "stickerset": {"_": "inputStickerSetID", "id": "-9000000000000000001", "access_hash": 42}},
    {"_": "documentAttributeFilename", "file_name": "AnimatedSticker.tgs"},
    {"_": "documentAttributeSticker", "pFlags": {}, "alt": "", "stickerset": {"_": "inputStickerSetEmpty"}}
  ]
}`

func TestDecodeDocument(t *testing.T) {
	b, err := Default.EncodeJSON([]byte(sticker))
	if err != nil {
		t.Fatal(err)
	}
	d := mtproto.NewDecodeBuf(b)
	o := d.Object()
	if err := d.GetError(); err != nil {
		t.Fatal(err)
	}
	doc := o.(interface{ To_Document() *mtproto.Document }).To_Document()
	if doc.GetId() != 5368324170671202286 || doc.GetAccessHash() != 1234567 || string(doc.GetFileReference()) != "\x01\x02\x03" ||
		doc.GetMimeType() != "application/x-tgsticker" || doc.GetDcId() != 2 || doc.GetDate() != 1700000000 {
		t.Fatalf("document %v", doc)
	}
	if len(doc.GetThumbs()) != 2 || doc.GetThumbs()[0].GetPredicateName() != mtproto.Predicate_photoPathSize ||
		string(doc.GetThumbs()[0].GetBytes()) != "\x04\x05\x06" || doc.GetThumbs()[1].GetType() != "m" ||
		doc.GetThumbs()[1].GetW() != 128 {
		t.Fatalf("thumbs %v", doc.GetThumbs())
	}
	if len(doc.GetVideoThumbs()) != 1 || doc.GetVideoThumbs()[0].GetType() != "f" {
		t.Fatalf("video thumbs %v", doc.GetVideoThumbs())
	}
	attrs := doc.GetAttributes()
	if len(attrs) != 4 || attrs[3].GetStickerset().GetPredicateName() != mtproto.Predicate_inputStickerSetEmpty || attrs[1].GetPredicateName() != mtproto.Predicate_documentAttributeSticker || !attrs[1].GetMask() ||
		attrs[1].GetAlt() != "😀" || attrs[1].GetStickerset().GetId() != -9000000000000000001 || attrs[1].GetStickerset().GetAccessHash() != 42 ||
		attrs[2].GetFileName() != "AnimatedSticker.tgs" {
		t.Fatalf("attributes %v", attrs)
	}
}

// A long string and bytes over 253 take TL's long forms; a Bool and a
// vector of longs round-trip; a missing field, an unknown constructor and a
// wrong type are errors, never a guess.
func TestEncodeForms(t *testing.T) {
	long := strings.Repeat("ж", 300)
	o, err := Default.Decode(map[string]any{
		"_":         "emojiKeyword",
		"keyword":   long,
		"emoticons": []any{"😀", "😃"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if k := o.(interface{ To_EmojiKeyword() *mtproto.EmojiKeyword }).To_EmojiKeyword(); k.GetKeyword() != long || len(k.GetEmoticons()) != 2 {
		t.Fatalf("keyword %v", k)
	}
	for name, v := range map[string]any{
		"unknown":       map[string]any{"_": "noSuchThing"},
		"missing field": map[string]any{"_": "emojiKeyword", "keyword": "a"},
		"wrong type":    map[string]any{"_": "emojiKeyword", "keyword": 5, "emoticons": []any{}},
		"not an object": []any{1},
		"bad bytes":     map[string]any{"_": "photoStrippedSize", "type": "i", "bytes": "AQID"},
		"a byte of 256": map[string]any{"_": "photoStrippedSize", "type": "i", "bytes": []any{json.Number("256")}},
	} {
		if _, err := Default.Decode(v); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A field of a boxed type with a namespace (help.PeerColorOption).
func TestEncodeNamespacedType(t *testing.T) {
	o, err := Default.Decode(map[string]any{
		"_": "help.peerColors", "hash": json.Number("5"),
		"colors": []any{map[string]any{"_": "help.peerColorOption", "pFlags": map[string]any{}, "color_id": json.Number("7")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := o.(interface{ To_Help_PeerColors() *mtproto.Help_PeerColors }).To_Help_PeerColors()
	if c.GetHash() != 5 || len(c.GetColors()) != 1 || c.GetColors()[0].GetColorId() != 7 {
		t.Fatalf("peer colors %v", c)
	}
}

func TestSchema(t *testing.T) {
	for name, id := range map[string]uint32{
		"document": 0x8fd4c4d8, "stickerSet": 0x2dd14edc, "availableReaction": 0xc077ec01,
		"availableEffect": 0x93c3e27e, "wallPaper": 0xa437c3ed, "timezone": 0xff9289f5,
	} {
		if c := Default.Constructor(name); c == nil || c.ID != id {
			t.Errorf("%s: %+v, want %#x", name, c, id)
		}
	}
}
