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
)

// MessagesGetArchivedStickers
// messages.getArchivedStickers#57f17692 flags:# masks:flags.0?true emojis:flags.1?true offset_id:long limit:int = messages.ArchivedStickers;
//
// The archived sets of a kind, the last archived first: limit of them
// after offset_id (none: from the first), each with its cover (a custom
// emoji set with all its documents, stickerSetFullCovered, as Telegram
// gives emoji sets), and count, the number of all of them. Limit 0 is the
// count alone: Android asks so for it (MediaDataController, req.limit = 0,
// docs/tasks/030-research/methods.md).
func (c *CatalogCore) MessagesGetArchivedStickers(in *mtproto.TLMessagesGetArchivedStickers) (*mtproto.Messages_ArchivedStickers, error) {
	kind := installed.Stickers
	switch {
	case in.GetEmojis():
		kind = installed.Emojis
	case in.GetMasks():
		kind = installed.Masks
	}
	rows, err := c.svcCtx.Installed.Archived(c.ctx, c.MD.UserId, kind)
	if err != nil {
		c.Logger.Errorf("messages.getArchivedStickers - error: %v", err)
		return nil, err
	}
	from := 0
	if in.GetOffsetId() != 0 {
		from = len(rows)
		for i, r := range rows {
			if r.SetID == in.GetOffsetId() {
				from = i + 1
				break
			}
		}
	}
	limit := int(in.GetLimit())
	if limit < 0 || limit > maxArchived {
		limit = maxArchived
	}
	sets := []*mtproto.StickerSetCovered{}
	for _, r := range rows[from:min(from+limit, len(rows))] {
		set, err := c.svcCtx.Catalog.SetByID(c.ctx, r.SetID)
		if err != nil {
			return nil, err
		}
		if set == nil {
			continue
		}
		ms, err := c.decodeSet("messages.getArchivedStickers", set)
		if err != nil {
			return nil, err
		}
		s := asArchived(ms, r.InstalledAt)
		if kind == installed.Emojis {
			sets = append(sets, mtproto.MakeTLStickerSetFullCovered(&mtproto.StickerSetCovered{
				Set: s, Packs: ms.GetPacks(), Keywords: ms.GetKeywords(), Documents: ms.GetDocuments(),
			}).To_StickerSetCovered())
			continue
		}
		if len(ms.GetDocuments()) == 0 {
			sets = append(sets, mtproto.MakeTLStickerSetNoCovered(&mtproto.StickerSetCovered{Set: s}).To_StickerSetCovered())
			continue
		}
		sets = append(sets, mtproto.MakeTLStickerSetCovered(&mtproto.StickerSetCovered{Set: s, Cover: ms.GetDocuments()[0]}).To_StickerSetCovered())
	}
	return mtproto.MakeTLMessagesArchivedStickers(&mtproto.Messages_ArchivedStickers{
		Count: int32(len(rows)),
		Sets:  sets,
	}).To_Messages_ArchivedStickers(), nil
}

// maxArchived bounds one page; iOS asks for 200 (docs/tasks/030-research).
const maxArchived = 200
