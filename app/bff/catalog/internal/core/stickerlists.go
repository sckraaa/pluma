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
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
)

// stickerList is a list of stickers of the account as the clients get it:
// the stickers in order, the date of each, their packs and their hash.
type stickerList struct {
	stickers []*mtproto.Document
	dates    []int32
	packs    []*mtproto.StickerPack
	hash     int64
}

// stickerList reads list of the account. A sticker whose document is gone
// is left out; the packs are read only when the client's hash is not the
// list's.
func (c *CatalogCore) stickerList(method string, list stickerlists.List, clientHash int64) (*stickerList, bool, error) {
	entries, err := c.svcCtx.StickerLists.Entries(c.ctx, c.MD.UserId, list)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, false, err
	}
	out := &stickerList{stickers: []*mtproto.Document{}, dates: []int32{}, packs: []*mtproto.StickerPack{}}
	if len(entries) > 0 {
		ids := make([]int64, len(entries))
		for i, e := range entries {
			ids[i] = e.DocumentID
		}
		docs, err := c.svcCtx.Media.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{IdList: ids})
		if err != nil {
			c.Logger.Errorf("%s - media: %v", method, err)
			return nil, false, err
		}
		out.stickers, out.dates = stickerlists.InOrder(entries, docs.GetDatas())
	}
	out.hash = stickerlists.Hash(out.stickers)
	if notModified(clientHash, out.hash) {
		return out, true, nil
	}
	if out.packs, err = c.stickerPacks(method, out.stickers); err != nil {
		return nil, false, err
	}
	return out, false, nil
}

// stickerPacks are the packs of stickers: the emoji of each by the packs of
// its set in the catalog, or by its own alt for a set the catalog has not.
func (c *CatalogCore) stickerPacks(method string, stickers []*mtproto.Document) ([]*mtproto.StickerPack, error) {
	setPacks := map[int64][]*mtproto.StickerPack{}
	for _, d := range stickers {
		set := stickerlists.StickerAttribute(d).GetStickerset()
		if set.GetPredicateName() != mtproto.Predicate_inputStickerSetID {
			continue
		}
		if _, ok := setPacks[set.GetId()]; ok {
			continue
		}
		stored, err := c.svcCtx.Catalog.SetByID(c.ctx, set.GetId())
		if err != nil {
			c.Logger.Errorf("%s - the catalog: %v", method, err)
			return nil, err
		}
		setPacks[set.GetId()] = nil
		if stored == nil {
			continue
		}
		ms, err := c.decodeSet(method, stored)
		if err != nil {
			return nil, err
		}
		setPacks[set.GetId()] = ms.GetPacks()
	}
	return stickerlists.Packs(stickers, func(d *mtproto.Document) []string {
		return stickerlists.SetEmoticons(d, setPacks[stickerlists.StickerAttribute(d).GetStickerset().GetId()])
	}), nil
}

// changeStickerList adds the sticker id names to list, or takes it off with
// remove. A sticker to add is a document with its access hash that clients
// show as a sticker; anything else is STICKER_ID_INVALID
// (core.telegram.org/method/messages.faveSticker and
// messages.saveRecentSticker). A sticker is taken off by its id alone: its
// document may be gone.
func (c *CatalogCore) changeStickerList(method string, list stickerlists.List, id *mtproto.InputDocument, remove bool) error {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return err
	}
	if id.GetPredicateName() != mtproto.Predicate_inputDocument || id.GetId() == 0 {
		err := mtproto.ErrStickerIdInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return err
	}
	if remove {
		if err := c.svcCtx.StickerLists.Remove(c.ctx, c.MD.UserId, list, id.GetId()); err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return err
		}
		return nil
	}
	doc, err := c.svcCtx.Media.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: id.GetId()})
	if err != nil {
		c.Logger.Errorf("%s - media: %v", method, err)
		return err
	}
	if stickerlists.StickerAttribute(doc) == nil || doc.GetAccessHash() != id.GetAccessHash() {
		err = mtproto.ErrStickerIdInvalid
		c.Logger.Errorf("%s - document %d: %v", method, id.GetId(), err)
		return err
	}
	if err := c.svcCtx.StickerLists.Add(c.ctx, c.MD.UserId, list, id.GetId()); err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return err
	}
	return nil
}

// recentList is the recent list messages.getRecentStickers,
// saveRecentSticker and clearRecentStickers name by attached.
func recentList(attached bool) stickerlists.List {
	if attached {
		return stickerlists.RecentAttached
	}
	return stickerlists.Recent
}
