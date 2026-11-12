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
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
)

// MessagesGetAttachedStickers
// messages.getAttachedStickers#cc5b67cc media:InputStickeredMedia = Vector<StickerSetCovered>;
//
// The sets of the stickers put on a photo or a video (pkg/catalog/attached),
// each once, in the order of the stickers: clients ask for them from media
// marked as having stickers and open the one set, or list several
// (Android StickersAlert.java:414, Telegram Desktop
// api_attached_stickers.cpp:27). A sticker of no set, or of a set that is
// gone, adds none; media without stickers answers none.
func (c *CatalogCore) MessagesGetAttachedStickers(in *mtproto.TLMessagesGetAttachedStickers) (*mtproto.Vector_StickerSetCovered, error) {
	const method = "messages.getAttachedStickers"
	var mediaID int64
	switch in.GetMedia().GetPredicateName() {
	case mtproto.Predicate_inputStickeredMediaPhoto:
		mediaID = in.GetMedia().GetId_INPUTPHOTO().GetId()
	case mtproto.Predicate_inputStickeredMediaDocument:
		mediaID = in.GetMedia().GetId_INPUTDOCUMENT().GetId()
	}
	if mediaID == 0 {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrMediaInvalid)
		return nil, mtproto.ErrMediaInvalid
	}
	ids, err := c.svcCtx.Attached.Of(c.ctx, mediaID)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	docs, err := c.documentsInOrder(method, ids)
	if err != nil {
		return nil, err
	}
	out := &mtproto.Vector_StickerSetCovered{Datas: []*mtproto.StickerSetCovered{}}
	seen := map[int64]bool{}
	for _, d := range docs {
		ref := stickerlists.StickerAttribute(d).GetStickerset()
		if ref.GetPredicateName() != mtproto.Predicate_inputStickerSetID || seen[ref.GetId()] {
			continue
		}
		seen[ref.GetId()] = true
		set, err := c.svcCtx.Catalog.SetByID(c.ctx, ref.GetId())
		if err != nil {
			c.Logger.Errorf("%s - the catalog: %v", method, err)
			return nil, err
		}
		if set == nil {
			continue
		}
		ms, err := c.viewOf(method, set)
		if err != nil {
			return nil, err
		}
		out.Datas = append(out.Datas, covered(ms))
	}
	return out, nil
}
