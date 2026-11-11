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

// MessagesGetFavedStickers
// messages.getFavedStickers#4f1aaa9 hash:long = messages.FavedStickers;
//
// The stickers the account faved, the last faved first, with the packs of
// their emoji; a hash equal to theirs is favedStickersNotModified.
func (c *CatalogCore) MessagesGetFavedStickers(in *mtproto.TLMessagesGetFavedStickers) (*mtproto.Messages_FavedStickers, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getFavedStickers - error: %v", err)
		return nil, err
	}
	l, same, err := c.stickerList("messages.getFavedStickers", stickerlists.Faved, in.GetHash())
	if err != nil {
		return nil, err
	}
	if same {
		return mtproto.MakeTLMessagesFavedStickersNotModified(nil).To_Messages_FavedStickers(), nil
	}
	return mtproto.MakeTLMessagesFavedStickers(&mtproto.Messages_FavedStickers{
		Hash:     l.hash,
		Packs:    l.packs,
		Stickers: l.stickers,
	}).To_Messages_FavedStickers(), nil
}
