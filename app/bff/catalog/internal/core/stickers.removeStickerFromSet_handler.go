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

// StickersRemoveStickerFromSet
// stickers.removeStickerFromSet#f7760f51 sticker:InputDocument = messages.StickerSet;
//
// Takes a sticker out of the set it is in, a set the account made. The
// sticker stays a document: messages that carry it still show it. A set
// left without stickers stays, empty: Android uninstalls it itself after
// the answer (StickersAlert.java:253) and the account may add to it again.
// The accounts that have the set get updateStickerSets.
func (c *CatalogCore) StickersRemoveStickerFromSet(in *mtproto.TLStickersRemoveStickerFromSet) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.removeStickerFromSet"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	id, err := c.ownSetOf(method, in.GetSticker())
	if err != nil {
		return nil, err
	}
	return c.changeSet(method, id, func(ch *usersets.Change) error {
		if _, err := c.ownSticker(ch, in.GetSticker()); err != nil {
			return err
		}
		usersets.Remove(ch.MS, in.GetSticker().GetId())
		ch.Drop(in.GetSticker().GetId())
		return nil
	})
}
