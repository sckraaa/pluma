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

// StickersChangeStickerPosition
// stickers.changeStickerPosition#ffb6d4ca sticker:InputDocument position:int = messages.StickerSet;
//
// Moves a sticker of a set the account made to position, counted from 0
// (Telegram Desktop sends the index the sticker was dropped at,
// sticker_set_box.cpp:1593); a position past the end is the end. The
// accounts that have the set get updateStickerSets.
func (c *CatalogCore) StickersChangeStickerPosition(in *mtproto.TLStickersChangeStickerPosition) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.changeStickerPosition"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
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
		to := min(max(int(in.GetPosition()), 0), len(ch.MS.GetDocuments())-1)
		if at == to {
			return usersets.ErrUnchanged
		}
		usersets.Move(ch.MS, in.GetSticker().GetId(), to)
		return nil
	})
}
