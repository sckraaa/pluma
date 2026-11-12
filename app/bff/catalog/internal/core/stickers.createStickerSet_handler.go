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
	"errors"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
)

// StickersCreateStickerSet
// stickers.createStickerSet#9021ab67 flags:# masks:flags.0?true emojis:flags.5?true text_color:flags.6?true user_id:InputUser title:string short_name:string thumb:flags.2?InputDocument stickers:Vector<InputStickerSetItem> software:flags.3?string = messages.StickerSet;
//
// Makes a set of stickers, masks or custom emoji of the account (task 084,
// ADR 0053) out of documents it uploaded or already has. Every sticker is
// checked (stickerfile) and stored as a document of the set before the set
// is: a failure leaves no set. An empty short name is made of the title
// (freeShortName). The set is installed for the account at once, so that
// every client shows it among the account's sets: Android installs it
// itself after the answer (StickerMakerView.java:1513), iOS only opens it
// (MediaEditorScreen.swift:8274). Its devices get updateNewStickerSet.
func (c *CatalogCore) StickersCreateStickerSet(in *mtproto.TLStickersCreateStickerSet) (*mtproto.Messages_StickerSet, error) {
	const method = "stickers.createStickerSet"
	fail := func(err error) (*mtproto.Messages_StickerSet, error) {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	kind := installed.Stickers
	switch {
	case c.MD.IsBot:
		return fail(mtproto.ErrBotMethodInvalid)
	case !c.selfUser(in.GetUserId()):
		return fail(mtproto.ErrUserIdInvalid)
	case in.GetMasks() && in.GetEmojis():
		return fail(errPackTypeInvalid)
	case !usersets.ValidTitle(in.GetTitle()):
		return fail(mtproto.ErrPackTitleInvalid)
	case in.GetEmojis():
		kind = installed.Emojis
	case in.GetMasks():
		kind = installed.Masks
	}
	switch {
	case len(in.GetStickers()) == 0:
		return fail(mtproto.ErrStickersEmpty)
	case len(in.GetStickers()) > usersets.Limit(kind):
		return fail(mtproto.ErrStickersTooMuch)
	}
	name, made := in.GetShortName(), false
	if name == "" {
		var err error
		if name, err = c.freeShortName(method, in.GetTitle()); err != nil {
			return nil, err
		}
		made = true
	} else {
		if !usersets.ValidShortName(name) {
			return fail(mtproto.ErrPackShortNameInvalid)
		}
		taken, err := c.svcCtx.UserSets.ShortNameTaken(c.ctx, name)
		if err != nil {
			return fail(err)
		}
		if taken {
			return fail(mtproto.ErrPackShortNameOccupied)
		}
	}

	id, accessHash, err := newSetID()
	if err != nil {
		return fail(err)
	}
	ms := usersets.New(id, accessHash, in.GetTitle(), name, kind, in.GetTextColor())
	shape := shapeOf(ms.GetSet())
	var links []usersets.Link
	for _, item := range in.GetStickers() {
		// A document given twice is one sticker: the second is not stored.
		if slices.ContainsFunc(links, func(l usersets.Link) bool { return l.SourceID == item.GetDocument().GetId() }) {
			continue
		}
		st, link, err := c.makeSticker(method, shape, item)
		if err != nil {
			return nil, err
		}
		usersets.Insert(ms, len(ms.GetDocuments()), st)
		links = append(links, link)
	}
	var thumbID int64
	if in.GetThumb() != nil && kind != installed.Emojis {
		docID, size, err := c.makeThumb(method, in.GetThumb())
		if err != nil {
			return nil, err
		}
		thumbID = docID
		ms.Set.Thumbs, ms.Set.ThumbDcId, ms.Set.ThumbVersion = []*mtproto.PhotoSize{size}, mtproto.MakeFlagsInt32(1), mtproto.MakeFlagsInt32(1)
	}

	set, err := c.svcCtx.UserSets.Create(c.ctx, c.MD.UserId, ms, thumbID, links, time.Now().Unix())
	if errors.Is(err, usersets.ErrShortNameOccupied) && made {
		// Taken since it was made: once more with another.
		if name, err = c.freeShortName(method, in.GetTitle()); err != nil {
			return nil, err
		}
		ms.Set.ShortName = name
		set, err = c.svcCtx.UserSets.Create(c.ctx, c.MD.UserId, ms, thumbID, links, time.Now().Unix())
	}
	if errors.Is(err, usersets.ErrShortNameOccupied) {
		return fail(mtproto.ErrPackShortNameOccupied)
	}
	if err != nil {
		return fail(err)
	}

	// The set is made by now: one that could not be installed is still
	// answered, and the account installs it as any set.
	view, err := c.decodeSet(method, set)
	if err != nil {
		return nil, err
	}
	if err := c.svcCtx.Installed.Install(c.ctx, c.MD.UserId, kind, set.ID, false, func(installedAt int64) *mtproto.Update {
		return mtproto.MakeTLUpdateNewStickerSet(&mtproto.Update{
			Stickerset_MESSAGES_STICKERSET: asInstalled(view, installedAt),
		}).To_Update()
	}); err != nil {
		c.Logger.Errorf("%s - installing set %d: %v", method, set.ID, err)
	}
	return c.viewOf(method, set)
}
