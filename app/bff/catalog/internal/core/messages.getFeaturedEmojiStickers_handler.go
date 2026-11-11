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

// MessagesGetFeaturedEmojiStickers
// messages.getFeaturedEmojiStickers#ecf6736 hash:long = messages.FeaturedStickers;
//
// The trending custom emoji sets. From the catalog; nothing while it is not imported.
func (c *CatalogCore) MessagesGetFeaturedEmojiStickers(in *mtproto.TLMessagesGetFeaturedEmojiStickers) (*mtproto.Messages_FeaturedStickers, error) {
	answer := &mtproto.Messages_FeaturedStickers{}
	hash, found, err := c.stored("messages.getFeaturedEmojiStickers", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyFeatured(), nil
	}
	if hash, err = c.featuredFor("messages.getFeaturedEmojiStickers", answer, hash); err != nil {
		return nil, err
	}
	if notModified(in.GetHash(), hash) {
		return featuredNotModified(answer), nil
	}
	answer.Hash = hash
	return answer, nil
}
