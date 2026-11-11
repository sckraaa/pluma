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

// Package usersets keeps the sticker and custom emoji sets users make
// (task 084, ADR 0053). A set lives beside the catalog's in
// catalog_sticker_sets, as the messages.stickerSet clients get, with the
// account that made it; its stickers are documents of their own, made
// from the files the user gave (user_sticker_set_documents). This file
// builds and changes the messages.stickerSet; store.go keeps it.
package usersets

import (
	"hash/crc32"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"google.golang.org/protobuf/proto"
)

// The most stickers a set a user made holds, as the clients take it:
// Android will not add to a set of 120 (StickersAlert.STICKERS_MAX_COUNT,
// StickersAlert.java:117), Telegram Desktop to one of 120 stickers or 200
// custom emoji (kStickersInOwnedSetMax, kEmojiInOwnedSetMax,
// api_stickers_creator.h:33).
const (
	MaxStickers = 120
	MaxEmojis   = 200
)

// Limit is the most stickers a set of kind holds.
func Limit(kind installed.Kind) int {
	if kind == installed.Emojis {
		return MaxEmojis
	}
	return MaxStickers
}

// Sticker is a sticker to put in a set: its document, already of the set
// (Document), the emoji it stands for and its keywords.
type Sticker struct {
	Document *mtproto.Document
	Emoji    []string
	Keywords []string
}

// New is an empty set.
func New(id, accessHash int64, title, shortName string, kind installed.Kind, textColor bool) *mtproto.Messages_StickerSet {
	return mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{
			Id:         id,
			AccessHash: accessHash,
			Title:      title,
			ShortName:  shortName,
			Masks:      kind == installed.Masks,
			Emojis:     kind == installed.Emojis,
			TextColor:  kind == installed.Emojis && textColor,
		}).To_StickerSet(),
		Packs:     []*mtproto.StickerPack{},
		Keywords:  []*mtproto.StickerKeyword{},
		Documents: []*mtproto.Document{},
	}).To_Messages_StickerSet()
}

// Index is the place of the sticker id in ms, -1 if it is not there.
func Index(ms *mtproto.Messages_StickerSet, id int64) int {
	return slices.IndexFunc(ms.GetDocuments(), func(d *mtproto.Document) bool { return d.GetId() == id })
}

// Emoji is the emoji the sticker id of ms stands for, in the order of the
// set's packs.
func Emoji(ms *mtproto.Messages_StickerSet, id int64) []string {
	var out []string
	for _, p := range ms.GetPacks() {
		if slices.Contains(p.GetDocuments(), id) {
			out = append(out, p.GetEmoticon())
		}
	}
	return out
}

// Keywords is the keywords of the sticker id of ms.
func Keywords(ms *mtproto.Messages_StickerSet, id int64) []string {
	for _, k := range ms.GetKeywords() {
		if k.GetDocumentId() == id {
			return k.GetKeyword()
		}
	}
	return nil
}

// Insert puts st in ms at position at (the end for one past it).
func Insert(ms *mtproto.Messages_StickerSet, at int, st Sticker) {
	at = min(max(at, 0), len(ms.Documents))
	ms.Documents = slices.Insert(ms.Documents, at, st.Document)
	setEmoji(ms, st.Document.GetId(), st.Emoji)
	setKeywords(ms, st.Document.GetId(), st.Keywords)
}

// Remove takes the sticker id out of ms.
func Remove(ms *mtproto.Messages_StickerSet, id int64) {
	ms.Documents = slices.DeleteFunc(ms.Documents, func(d *mtproto.Document) bool { return d.GetId() == id })
	setEmoji(ms, id, nil)
	setKeywords(ms, id, nil)
	if ms.GetSet().GetThumbDocumentId().GetValue() == id {
		ms.Set.ThumbDocumentId = nil
	}
}

// Move puts the sticker id of ms at position to.
func Move(ms *mtproto.Messages_StickerSet, id int64, to int) {
	at := Index(ms, id)
	if at < 0 {
		return
	}
	d := ms.Documents[at]
	ms.Documents = slices.Delete(ms.Documents, at, at+1)
	ms.Documents = slices.Insert(ms.Documents, min(max(to, 0), len(ms.Documents)), d)
	rebuildPacks(ms, nil)
}

// SetEmoji makes the sticker id of ms stand for emoji.
func SetEmoji(ms *mtproto.Messages_StickerSet, id int64, emoji []string) { setEmoji(ms, id, emoji) }

// SetKeywords gives the sticker id of ms its keywords.
func SetKeywords(ms *mtproto.Messages_StickerSet, id int64, keywords []string) {
	setKeywords(ms, id, keywords)
}

func setEmoji(ms *mtproto.Messages_StickerSet, id int64, emoji []string) {
	rebuildPacks(ms, map[int64][]string{id: emoji})
}

// rebuildPacks makes the packs of ms again: for each emoji, the stickers
// that stand for it in the order of the set, the emoji in the order the
// set first names them. changed gives stickers other emoji than they had.
func rebuildPacks(ms *mtproto.Messages_StickerSet, changed map[int64][]string) {
	var order []string
	byEmoji := map[string][]int64{}
	for _, d := range ms.GetDocuments() {
		emoji, ok := changed[d.GetId()]
		if !ok {
			emoji = Emoji(ms, d.GetId())
		}
		for _, e := range emoji {
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
	ms.Packs = packs
}

func setKeywords(ms *mtproto.Messages_StickerSet, id int64, keywords []string) {
	ms.Keywords = slices.DeleteFunc(ms.Keywords, func(k *mtproto.StickerKeyword) bool { return k.GetDocumentId() == id })
	if len(keywords) > 0 {
		ms.Keywords = append(ms.Keywords, mtproto.MakeTLStickerKeyword(&mtproto.StickerKeyword{DocumentId: id, Keyword: keywords}).To_StickerKeyword())
	}
	if ms.Keywords == nil {
		ms.Keywords = []*mtproto.StickerKeyword{}
	}
}

// Seal sets what follows from the stickers of ms: their count and the
// set's hash, which clients compare to tell a set that changed
// (messages.getStickerSet, and the hash of messages.getAllStickers over
// the hashes of the sets). The hash is of everything in the set and never
// 0, which clients send for "no set yet".
func Seal(ms *mtproto.Messages_StickerSet) error {
	if ms.Documents == nil {
		ms.Documents = []*mtproto.Document{}
	}
	ms.Set.Count = int32(len(ms.Documents))
	ms.Set.Hash = 0
	// What an account has of the set is not the set's.
	ms.Set.InstalledDate, ms.Set.Archived, ms.Set.Creator = nil, false, false
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(ms)
	if err != nil {
		return err
	}
	h := int32(crc32.ChecksumIEEE(data))
	if h == 0 {
		h = 1
	}
	ms.Set.Hash = h
	return nil
}
