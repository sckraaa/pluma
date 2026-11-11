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

// MessagesFaveSticker
// messages.faveSticker#b9ffc55b id:InputDocument unfave:Bool = Bool;
//
// Faves a sticker at the top of the account's faved stickers, dropping the
// oldest past stickerlists.FavedLimit, or unfaves it; every device of the
// account gets updateFavedStickers. Android and Telegram Desktop load the
// list again after the answer (MediaDataController.addRecentSticker,
// api_toggling_media.cpp ToggleFavedSticker).
func (c *CatalogCore) MessagesFaveSticker(in *mtproto.TLMessagesFaveSticker) (*mtproto.Bool, error) {
	if err := c.changeStickerList("messages.faveSticker", stickerlists.Faved, in.GetId(), mtproto.FromBool(in.GetUnfave())); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
