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

// MessagesClearRecentStickers
// messages.clearRecentStickers#8999602d flags:# attached:flags.0?true = Bool;
//
// Empties the account's recent stickers (or those attached to photos and
// videos); every device of the account gets updateRecentStickers.
func (c *CatalogCore) MessagesClearRecentStickers(in *mtproto.TLMessagesClearRecentStickers) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.clearRecentStickers - error: %v", err)
		return nil, err
	}
	if err := c.svcCtx.StickerLists.Clear(c.ctx, c.MD.UserId, recentList(in.GetAttached())); err != nil {
		c.Logger.Errorf("messages.clearRecentStickers - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
