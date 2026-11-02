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

// MessagesGetEmojiStickerGroups
// messages.getEmojiStickerGroups#1dd840f5 hash:int = messages.EmojiGroups;
//
// The categories of the sticker search. From the catalog; nothing while it is not imported.
func (c *CatalogCore) MessagesGetEmojiStickerGroups(in *mtproto.TLMessagesGetEmojiStickerGroups) (*mtproto.Messages_EmojiGroups, error) {
	answer := &mtproto.Messages_EmojiGroups{}
	hash, found, err := c.stored("messages.getEmojiStickerGroups", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyEmojiGroups(), nil
	}
	if notModified(int64(in.GetHash()), hash) {
		return mtproto.MakeTLMessagesEmojiGroupsNotModified(nil).To_Messages_EmojiGroups(), nil
	}
	return answer, nil
}
