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
	"slices"

	"github.com/teamgram/proto/mtproto"
)

// MessagesGetMyStickers
// messages.getMyStickers#d0b5e1fc offset_id:long limit:int = messages.MyStickers;
//
// The sets the account made, the last made first: limit of them after the
// set offset_id (none: from the first), and count, the number of all of
// them. Android and iOS list them to add a sticker to one
// (ContentPreviewViewer.java:2306, ImportStickers.swift:546); Android asks
// for the next page from the last set's id while a page is full. Each set
// comes covered as iOS reads it: stickerSetCovered or, a set of custom
// emoji, stickerSetFullCovered (ImportStickers.swift:561-600).
func (c *CatalogCore) MessagesGetMyStickers(in *mtproto.TLMessagesGetMyStickers) (*mtproto.Messages_MyStickers, error) {
	const method = "messages.getMyStickers"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	ids, err := c.svcCtx.UserSets.Owned(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	from := 0
	if in.GetOffsetId() != 0 {
		from = len(ids)
		if at := slices.Index(ids, in.GetOffsetId()); at >= 0 {
			from = at + 1
		}
	}
	limit := int(in.GetLimit())
	if limit <= 0 || limit > maxMySets {
		limit = maxMySets
	}
	sets := []*mtproto.StickerSetCovered{}
	for _, id := range ids[from:min(from+limit, len(ids))] {
		set, err := c.svcCtx.Catalog.SetByID(c.ctx, id)
		if err != nil {
			c.Logger.Errorf("%s - the catalog: %v", method, err)
			return nil, err
		}
		if set == nil {
			continue // deleted meanwhile
		}
		ms, err := c.viewOf(method, set)
		if err != nil {
			return nil, err
		}
		sets = append(sets, covered(ms))
	}
	return mtproto.MakeTLMessagesMyStickers(&mtproto.Messages_MyStickers{
		Count: int32(len(ids)),
		Sets:  sets,
	}).To_Messages_MyStickers(), nil
}

// maxMySets bounds one page; Android and iOS ask for 100.
const maxMySets = 100
