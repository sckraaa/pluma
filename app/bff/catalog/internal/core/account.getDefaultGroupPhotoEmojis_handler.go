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

// AccountGetDefaultGroupPhotoEmojis
// account.getDefaultGroupPhotoEmojis#915860ae hash:long = EmojiList;
//
// The custom emoji suggested for a group photo. From the catalog; nothing while it is not imported.
func (c *CatalogCore) AccountGetDefaultGroupPhotoEmojis(in *mtproto.TLAccountGetDefaultGroupPhotoEmojis) (*mtproto.EmojiList, error) {
	answer := &mtproto.EmojiList{}
	hash, found, err := c.stored("account.getDefaultGroupPhotoEmojis", "", answer)
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
