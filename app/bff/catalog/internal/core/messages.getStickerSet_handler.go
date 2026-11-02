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
)

// MessagesGetStickerSet
// messages.getStickerSet#c8a0ec74 stickerset:InputStickerSet hash:int = messages.StickerSet;
//
// A sticker or emoji set of the catalog, asked for by id and access hash,
// by short name or by kind (inputStickerSetAnimatedEmoji…). A set by name
// it lacks is fetched from Telegram if a bot is configured; a set Telegram
// has not either is STICKERSET_INVALID.
func (c *CatalogCore) MessagesGetStickerSet(in *mtproto.TLMessagesGetStickerSet) (*mtproto.Messages_StickerSet, error) {
	set, err := c.findSet("messages.getStickerSet", in.GetStickerset())
	if err != nil {
		return nil, err
	}
	if notModified(int64(in.GetHash()), int64(set.Hash)) {
		return mtproto.MakeTLMessagesStickerSetNotModified(nil).To_Messages_StickerSet(), nil
	}
	return c.decodeSet("messages.getStickerSet", set)
}
