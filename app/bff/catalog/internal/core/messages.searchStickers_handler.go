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

// MessagesSearchStickers
// messages.searchStickers#29b1c66a flags:# emojis:flags.0?true q:string emoticon:string lang_code:Vector<string> offset:int limit:int hash:long = messages.FoundStickers;
//
// The stickers (the custom emoji with emojis) found by a query
// (foundStickers), limit of them from offset, in a stable order, with the
// offset of the next page while there is one. The hash is of the page's
// stickers.
func (c *CatalogCore) MessagesSearchStickers(in *mtproto.TLMessagesSearchStickers) (*mtproto.Messages_FoundStickers, error) {
	const method = "messages.searchStickers"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	ids, err := c.foundStickers(method, in.GetEmojis(), in.GetQ(), in.GetEmoticon(), in.GetLangCode())
	if err != nil {
		return nil, err
	}
	offset, limit := int(in.GetOffset()), int(in.GetLimit())
	if offset < 0 || offset > len(ids) {
		offset = len(ids)
	}
	if limit <= 0 || limit > maxFoundStickers {
		limit = maxFoundStickers
	}
	end := min(offset+limit, len(ids))
	page := ids[offset:end]
	var next *mtproto.Messages_FoundStickers
	answer := &mtproto.Messages_FoundStickers{Hash: idsHash(page)}
	if end < len(ids) {
		answer.NextOffset = mtproto.MakeFlagsInt32(int32(end))
	}
	if notModified(in.GetHash(), answer.Hash) {
		next = mtproto.MakeTLMessagesFoundStickersNotModified(&mtproto.Messages_FoundStickers{NextOffset: answer.NextOffset}).To_Messages_FoundStickers()
		return next, nil
	}
	if answer.Stickers, err = c.documentsInOrder(method, page); err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesFoundStickers(answer).To_Messages_FoundStickers(), nil
}
