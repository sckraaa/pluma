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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
)

// StickersReplaceSticker
// stickers.replaceSticker#4696459a sticker:InputDocument new_sticker:InputStickerSetItem = messages.StickerSet;
//
// Puts a new sticker in the place of one of a set the account made
// (Android and iOS: "edit sticker", StickerMakerView.java:1475,
// ImportStickers.swift:524). The old one stays a document. The accounts
// that have the set get updateStickerSets.
func (c *CatalogCore) StickersReplaceSticker(in *mtproto.TLStickersReplaceSticker) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.replaceSticker"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	id, err := c.ownSetOf(method, in.GetSticker())
	if err != nil {
		return nil, err
	}
	set, err := c.svcCtx.Catalog.SetByID(c.ctx, id)
	if err != nil {
		c.Logger.Errorf("%s - the catalog: %v", method, err)
		return nil, err
	}
	if set == nil || set.OwnerID != c.MD.UserId {
		c.Logger.Errorf("%s - set %d: %v", method, id, mtproto.ErrStickerInvalid)
		return nil, mtproto.ErrStickerInvalid
	}
	ms, err := c.decodeSet(method, set)
	if err != nil {
		return nil, err
	}
	st, link, err := c.makeSticker(method, shapeOf(ms.GetSet()), in.GetNewSticker())
	if err != nil {
		return nil, err
	}
	return c.changeSet(method, id, func(ch *usersets.Change) error {
		at, err := c.ownSticker(ch, in.GetSticker())
		if err != nil {
			return err
		}
		old := in.GetSticker().GetId()
		usersets.Remove(ch.MS, old)
		ch.Drop(old)
		usersets.Insert(ch.MS, at, st)
		ch.Add(link)
		return nil
	})
}
