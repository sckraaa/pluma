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

// StickersRenameStickerSet
// stickers.renameStickerSet#124b1c00 stickerset:InputStickerSet title:string = messages.StickerSet;
//
// Gives a set the account made another title, of 1 to 64 characters; its
// short name, and so its link, stay. The accounts that have the set get
// updateStickerSets.
func (c *CatalogCore) StickersRenameStickerSet(in *mtproto.TLStickersRenameStickerSet) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.renameStickerSet"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	if !usersets.ValidTitle(in.GetTitle()) {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrPackTitleInvalid)
		return nil, mtproto.ErrPackTitleInvalid
	}
	set, _, err := c.ownSet(method, in.GetStickerset())
	if err != nil {
		return nil, err
	}
	return c.changeSet(method, set.ID, func(ch *usersets.Change) error {
		switch {
		case ch.Set.OwnerID != c.MD.UserId:
			return errNotOwner
		case ch.MS.GetSet().GetTitle() == in.GetTitle():
			return usersets.ErrUnchanged
		}
		ch.MS.Set.Title = in.GetTitle()
		return nil
	})
}
