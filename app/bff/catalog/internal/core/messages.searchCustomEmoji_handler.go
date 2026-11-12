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

// MessagesSearchCustomEmoji
// messages.searchCustomEmoji#2c11c0d7 emoticon:string hash:long = EmojiList;
//
// The custom emoji that stand for the emoji of emoticon (one after
// another: the emoji of a category of the emoji panel, Android
// StickerCategoriesListView.java:1013, iOS SearchStickers.swift:1646), by
// their ids: the clients fetch the documents with
// messages.getCustomEmojiDocuments.
func (c *CatalogCore) MessagesSearchCustomEmoji(in *mtproto.TLMessagesSearchCustomEmoji) (*mtproto.EmojiList, error) {
	const method = "messages.searchCustomEmoji"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	ids, err := c.foundStickers(method, true, "", in.GetEmoticon(), nil)
	if err != nil {
		return nil, err
	}
	if len(ids) > maxFoundEmoji {
		ids = ids[:maxFoundEmoji]
	}
	hash := idsHash(ids)
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLEmojiListNotModified(nil).To_EmojiList(), nil
	}
	return mtproto.MakeTLEmojiList(&mtproto.EmojiList{Hash: hash, DocumentId: ids}).To_EmojiList(), nil
}
