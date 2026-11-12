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
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerfile"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
)

// StickersChangeSticker
// stickers.changeSticker#f5537ebc flags:# sticker:InputDocument emoji:flags.0?string mask_coords:flags.1?MaskCoords keywords:flags.2?string = messages.StickerSet;
//
// Changes the emoji, the keywords (separated by commas, not of a mask) or
// the place on a face (of a mask) of a sticker of a set the account made
// (core.telegram.org/method/stickers.changeSticker). The accounts that
// have the set get updateStickerSets.
func (c *CatalogCore) StickersChangeSticker(in *mtproto.TLStickersChangeSticker) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.changeSticker"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	var emoji []string
	if in.GetEmoji() != nil {
		var ok bool
		if emoji, ok = stickerfile.SplitEmoji(in.GetEmoji().GetValue()); !ok {
			c.Logger.Errorf("%s - error: %v", method, mtproto.ErrStickerEmojiInvalid)
			return nil, mtproto.ErrStickerEmojiInvalid
		}
	}
	id, err := c.ownSetOf(method, in.GetSticker())
	if err != nil {
		return nil, err
	}
	return c.changeSet(method, id, func(ch *usersets.Change) error {
		at, err := c.ownSticker(ch, in.GetSticker())
		if err != nil {
			return err
		}
		doc := ch.MS.GetDocuments()[at]
		masks := installed.KindOf(ch.MS.GetSet()) == installed.Masks
		if emoji != nil {
			usersets.SetEmoji(ch.MS, doc.GetId(), emoji)
			setAlt(doc, emoji[0])
		}
		if in.GetKeywords() != nil && !masks {
			usersets.SetKeywords(ch.MS, doc.GetId(), stickerfile.SplitKeywords(in.GetKeywords().GetValue()))
		}
		if in.GetMaskCoords() != nil && masks {
			for _, a := range doc.GetAttributes() {
				if a.GetPredicateName() == mtproto.Predicate_documentAttributeSticker {
					a.MaskCoords = in.GetMaskCoords()
				}
			}
		}
		return nil
	})
}
