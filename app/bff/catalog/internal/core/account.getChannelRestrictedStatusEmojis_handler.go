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

// AccountGetChannelRestrictedStatusEmojis
// account.getChannelRestrictedStatusEmojis#35a9e0d5 hash:long = EmojiList;
//
// The custom emoji a channel may not take as its status. From the catalog; nothing while it is not imported.
func (c *CatalogCore) AccountGetChannelRestrictedStatusEmojis(in *mtproto.TLAccountGetChannelRestrictedStatusEmojis) (*mtproto.EmojiList, error) {
	answer := &mtproto.EmojiList{}
	hash, found, err := c.stored("account.getChannelRestrictedStatusEmojis", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyEmojiList(), nil
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLEmojiListNotModified(nil).To_EmojiList(), nil
	}
	return answer, nil
}
