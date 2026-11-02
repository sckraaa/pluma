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

// MessagesGetOldFeaturedStickers
// messages.getOldFeaturedStickers#7ed094a1 offset:int limit:int hash:long = messages.FeaturedStickers;
//
// The trending sticker sets past the first page, from the catalog: offset
// and limit cut the list, which goes without a hash, as a page of it.
func (c *CatalogCore) MessagesGetOldFeaturedStickers(in *mtproto.TLMessagesGetOldFeaturedStickers) (*mtproto.Messages_FeaturedStickers, error) {
	answer := &mtproto.Messages_FeaturedStickers{}
	_, found, err := c.stored("messages.getOldFeaturedStickers", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyFeatured(), nil
	}
	sets := answer.GetSets()
	offset, limit := int(in.GetOffset()), int(in.GetLimit())
	if offset < 0 || offset > len(sets) {
		offset = len(sets)
	}
	end := len(sets)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return mtproto.MakeTLMessagesFeaturedStickers(&mtproto.Messages_FeaturedStickers{
		Count:  int32(len(sets)),
		Sets:   sets[offset:end],
		Unread: []int64{},
	}).To_Messages_FeaturedStickers(), nil
}
