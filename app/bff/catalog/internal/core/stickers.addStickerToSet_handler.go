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

// StickersAddStickerToSet
// stickers.addStickerToSet#8653febe stickerset:InputStickerSet sticker:InputStickerSetItem = messages.StickerSet;
//
// Adds a sticker to the end of a set the account made. A document the set
// already has a sticker of is not added again, and the set is answered as
// it is: Telegram Desktop tells "already in the set" by the count that did
// not grow (api_stickers_creator.cpp:207-214). A full set is
// STICKERPACK_STICKERS_TOO_MUCH. The accounts that have the set get
// updateStickerSets.
func (c *CatalogCore) StickersAddStickerToSet(in *mtproto.TLStickersAddStickerToSet) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.addStickerToSet"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	set, ms, err := c.ownSet(method, in.GetStickerset())
	if err != nil {
		return nil, err
	}
	// Whether the sticker may be added is found out before its file is
	// stored, and once more with the set locked.
	source := in.GetSticker().GetDocument().GetId()
	links, err := c.svcCtx.UserSets.Links(c.ctx, set.ID)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	for _, l := range links {
		if l.SourceID == source || l.DocumentID == source {
			return c.viewOf(method, set)
		}
	}
	shape := shapeOf(ms.GetSet())
	if len(ms.GetDocuments()) >= usersets.Limit(shape.kind) {
		c.Logger.Errorf("%s - set %d: %v", method, set.ID, errSetFull)
		return nil, mtproto.ErrStickerpackStickersTooMuch
	}
	st, link, err := c.makeSticker(method, shape, in.GetSticker())
	if err != nil {
		return nil, err
	}
	return c.changeSet(method, set.ID, func(ch *usersets.Change) error {
		switch {
		case ch.Set.OwnerID != c.MD.UserId:
			return errNotOwner
		case ch.SourceOf(source) != 0:
			return usersets.ErrUnchanged
		case len(ch.MS.GetDocuments()) >= usersets.Limit(shape.kind):
			return errSetFull
		}
		usersets.Insert(ch.MS, len(ch.MS.GetDocuments()), st)
		ch.Add(link)
		return nil
	})
}
