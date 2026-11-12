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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
)

// StickersDeleteStickerSet
// stickers.deleteStickerSet#87704394 stickerset:InputStickerSet = Bool;
//
// Deletes a set the account made, for everyone: its link no longer opens
// it, and it leaves the sets of every account that had it, whose devices
// get updateStickerSets. Its stickers stay documents, so that messages
// that carry them still show them. The set is deleted first: an account
// it could not be taken from by then no longer gets it among its sets
// (allStickers leaves out a set that is gone).
func (c *CatalogCore) StickersDeleteStickerSet(in *mtproto.TLStickersDeleteStickerSet) (*mtproto.Bool, error) {
	const method = "stickers.deleteStickerSet"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	set, ms, err := c.ownSet(method, in.GetStickerset())
	if err != nil {
		return nil, err
	}
	err = c.svcCtx.UserSets.Delete(c.ctx, set.ID, c.MD.UserId)
	if errors.Is(err, usersets.ErrNoSet) {
		c.Logger.Errorf("%s - set %d: %v", method, set.ID, err)
		return nil, mtproto.ErrStickersetInvalid
	}
	if err != nil {
		c.Logger.Errorf("%s - set %d: %v", method, set.ID, err)
		return nil, err
	}
	if err := c.svcCtx.Installed.Drop(c.ctx, set.ID, installed.KindOf(ms.GetSet())); err != nil {
		c.Logger.Errorf("%s - taking set %d from the accounts that had it: %v", method, set.ID, err)
	}
	return mtproto.BoolTrue, nil
}
