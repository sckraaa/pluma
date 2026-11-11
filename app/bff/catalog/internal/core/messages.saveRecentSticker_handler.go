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

// MessagesSaveRecentSticker
// messages.saveRecentSticker#392718f8 flags:# attached:flags.0?true id:InputDocument unsave:Bool = Bool;
//
// Puts a sticker at the top of the account's recent stickers (or of those
// attached to photos and videos), or takes it off with unsave; every device
// of the account gets updateRecentStickers. iOS and Web save a sticker they
// send this way (ManagedSynchronizeRecentlyUsedMediaOperations.swift,
// chat/input.ts); Android and Telegram Desktop only take one off, and a
// sticker they send gets to the list with the message
// (messages.sendMedia).
func (c *CatalogCore) MessagesSaveRecentSticker(in *mtproto.TLMessagesSaveRecentSticker) (*mtproto.Bool, error) {
	if err := c.changeStickerList("messages.saveRecentSticker", recentList(in.GetAttached()), in.GetId(), mtproto.FromBool(in.GetUnsave())); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
