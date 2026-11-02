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
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"google.golang.org/protobuf/proto"
)

// MessagesGetStickerSet
// messages.getStickerSet#c8a0ec74 stickerset:InputStickerSet hash:int = messages.StickerSet;
//
// A sticker or emoji set of the catalog, asked for by id and access hash,
// by short name or by kind (inputStickerSetAnimatedEmoji…); a set it does
// not have is STICKERSET_INVALID.
func (c *CatalogCore) MessagesGetStickerSet(in *mtproto.TLMessagesGetStickerSet) (*mtproto.Messages_StickerSet, error) {
	var (
		set *catalog.Set
		err error
		ask = in.GetStickerset()
	)
	switch ask.GetPredicateName() {
	case mtproto.Predicate_inputStickerSetID:
		set, err = c.svcCtx.Catalog.SetByID(c.ctx, ask.GetId())
		if set != nil && set.AccessHash != ask.GetAccessHash() {
			set = nil
		}
	case mtproto.Predicate_inputStickerSetShortName:
		set, err = c.svcCtx.Catalog.SetByShortName(c.ctx, ask.GetShortName())
	default:
		set, err = c.svcCtx.Catalog.SetByKind(c.ctx, catalog.KindOf(ask))
	}
	if err != nil {
		c.Logger.Errorf("messages.getStickerSet - the catalog: %v", err)
		return nil, err
	}
	if set == nil {
		return nil, mtproto.ErrStickersetInvalid
	}
	if notModified(int64(in.GetHash()), int64(set.Hash)) {
		return mtproto.MakeTLMessagesStickerSetNotModified(nil).To_Messages_StickerSet(), nil
	}
	answer := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(set.Data, answer); err != nil {
		c.Logger.Errorf("messages.getStickerSet - a stored set: %v", err)
		return nil, err
	}
	return answer, nil
}
