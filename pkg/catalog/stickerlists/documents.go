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

package stickerlists

import (
	"slices"

	"github.com/teamgram/proto/mtproto"
)

// StickerAttribute is the documentAttributeSticker of doc, nil for a
// document that is no sticker. The clients keep only such documents in
// these lists: Telegram Desktop skips a document without it
// (Stickers::specialSetReceived, document->sticker()) and fave or remove
// nothing else (api_toggling_media.cpp); Android likewise
// (MediaDataController.addRecentSticker, MessageObject.isStickerDocument).
func StickerAttribute(doc *mtproto.Document) *mtproto.DocumentAttribute {
	if doc.GetPredicateName() != mtproto.Predicate_document {
		return nil
	}
	for _, a := range doc.GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeSticker {
			return a
		}
	}
	return nil
}

// Hash is the hash of the ids of docs in their order
// (core.telegram.org/api/offsets#hash-generation, with unsigned shifts), as
// the clients count it for messages.getRecentStickers and
// messages.getFavedStickers (Android MediaDataController.calcDocumentsHash
// over at most 200 stickers, which no list exceeds; Telegram Desktop
// CountDocumentVectorHash of api_hash.cpp).
func Hash(docs []*mtproto.Document) int64 {
	var hash uint64
	for _, d := range docs {
		hash ^= hash >> 21
		hash ^= hash << 35
		hash ^= hash >> 4
		hash += uint64(d.GetId())
	}
	return int64(hash)
}

// InOrder returns the stickers of docs in the order of entries, with the
// date of each, leaving out the entries whose document is gone or no
// sticker.
func InOrder(entries []Entry, docs []*mtproto.Document) ([]*mtproto.Document, []int32) {
	byID := make(map[int64]*mtproto.Document, len(docs))
	for _, d := range docs {
		if StickerAttribute(d) != nil {
			byID[d.GetId()] = d
		}
	}
	out := make([]*mtproto.Document, 0, len(entries))
	dates := make([]int32, 0, len(entries))
	for _, e := range entries {
		if d, ok := byID[e.DocumentID]; ok {
			out = append(out, d)
			dates = append(dates, int32(e.UsedAt))
		}
	}
	return out, dates
}

// Packs are the stickerPacks of a list of stickers: for each emoji, the
// stickers of the list it stands for, in the list's order, the emoji in the
// order the list first names them. Telegram Desktop finds the stickers of
// an emoji typed in the message field by them (Stickers::setPackAndEmoji);
// Android and iOS read the emoji of the stickers themselves. emoticons
// gives the emoji of a sticker.
func Packs(docs []*mtproto.Document, emoticons func(doc *mtproto.Document) []string) []*mtproto.StickerPack {
	var order []string
	byEmoji := map[string][]int64{}
	for _, d := range docs {
		for _, e := range emoticons(d) {
			if _, ok := byEmoji[e]; !ok {
				order = append(order, e)
			}
			byEmoji[e] = append(byEmoji[e], d.GetId())
		}
	}
	packs := make([]*mtproto.StickerPack, 0, len(order))
	for _, e := range order {
		packs = append(packs, mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: e, Documents: byEmoji[e]}).To_StickerPack())
	}
	return packs
}

// SetEmoticons is the emoji of the sticker doc by the packs of its set, or
// by the alt of its documentAttributeSticker when the set does not name it.
// An emoji named twice counts once.
func SetEmoticons(doc *mtproto.Document, packs []*mtproto.StickerPack) []string {
	var out []string
	for _, p := range packs {
		for _, id := range p.GetDocuments() {
			if id == doc.GetId() {
				if p.GetEmoticon() != "" && !slices.Contains(out, p.GetEmoticon()) {
					out = append(out, p.GetEmoticon())
				}
				break
			}
		}
	}
	if len(out) == 0 {
		if alt := StickerAttribute(doc).GetAlt(); alt != "" {
			out = append(out, alt)
		}
	}
	return out
}
