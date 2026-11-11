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
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
)

// StickersSetStickerSetThumb
// stickers.setStickerSetThumb#a76a5392 flags:# stickerset:InputStickerSet thumb:flags.0?InputDocument thumb_document_id:flags.1?long = messages.StickerSet;
//
// Sets the thumbnail of a set the account made
// (core.telegram.org/method/stickers.setStickerSetThumb): of a set of
// custom emoji, one of its emoji (thumb_document_id; 0 for the first one,
// which is what clients show without it); of a set of stickers or masks, a
// file of its own (thumb), and none without it, when clients show the
// first sticker. The accounts that have the set get updateStickerSets.
func (c *CatalogCore) StickersSetStickerSetThumb(in *mtproto.TLStickersSetStickerSetThumb) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.setStickerSetThumb"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	set, ms, err := c.ownSet(method, in.GetStickerset())
	if err != nil {
		return nil, err
	}
	if installed.KindOf(ms.GetSet()) == installed.Emojis {
		emoji := in.GetThumbDocumentId().GetValue()
		return c.changeSet(method, set.ID, func(ch *usersets.Change) error {
			switch {
			case ch.Set.OwnerID != c.MD.UserId:
				return errNotOwner
			case emoji != 0 && usersets.Index(ch.MS, emoji) < 0:
				return errNoSticker
			case ch.MS.GetSet().GetThumbDocumentId().GetValue() == emoji:
				return usersets.ErrUnchanged
			case emoji == 0:
				ch.MS.Set.ThumbDocumentId = nil
			default:
				ch.MS.Set.ThumbDocumentId = mtproto.MakeFlagsInt64(emoji)
			}
			return nil
		})
	}
	var (
		thumbID int64
		size    *mtproto.PhotoSize
	)
	if in.GetThumb() != nil {
		if thumbID, size, err = c.makeThumb(method, in.GetThumb()); err != nil {
			return nil, err
		}
	}
	return c.changeSet(method, set.ID, func(ch *usersets.Change) error {
		switch {
		case ch.Set.OwnerID != c.MD.UserId:
			return errNotOwner
		case size == nil && ch.Set.ThumbDocumentID == 0:
			return usersets.ErrUnchanged
		}
		putThumb(ch, thumbID, size)
		return nil
	})
}
