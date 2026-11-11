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

// MessagesGetRecentStickers
// messages.getRecentStickers#9da9403b flags:# attached:flags.0?true hash:long = messages.RecentStickers;
//
// The stickers the account sent lately, or with attached those it attached
// to photos and videos, the last used first, with the date each was used
// and the packs of their emoji; a hash equal to theirs is
// recentStickersNotModified. Telegram Desktop takes the dates only when
// there is one for each sticker (Stickers::specialSetReceived).
func (c *CatalogCore) MessagesGetRecentStickers(in *mtproto.TLMessagesGetRecentStickers) (*mtproto.Messages_RecentStickers, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getRecentStickers - error: %v", err)
		return nil, err
	}
	l, same, err := c.stickerList("messages.getRecentStickers", recentList(in.GetAttached()), in.GetHash())
	if err != nil {
		return nil, err
	}
	if same {
		return mtproto.MakeTLMessagesRecentStickersNotModified(nil).To_Messages_RecentStickers(), nil
	}
	return mtproto.MakeTLMessagesRecentStickers(&mtproto.Messages_RecentStickers{
		Hash:     l.hash,
		Packs:    l.packs,
		Stickers: l.stickers,
		Dates:    l.dates,
	}).To_Messages_RecentStickers(), nil
}
