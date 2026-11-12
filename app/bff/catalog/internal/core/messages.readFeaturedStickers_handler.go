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

// MessagesReadFeaturedStickers
// messages.readFeaturedStickers#5b118126 id:Vector<long> = Bool;
//
// Marks trending sets as seen by the account: they leave the unread ones
// of messages.getFeaturedStickers and messages.getFeaturedEmojiStickers
// (featuredFor), and the account's other devices get
// updateReadFeaturedStickers, or updateReadFeaturedEmojiStickers for sets
// of custom emoji. The clients read both kinds with this method, a set by
// its id (Android MediaDataController.java:2501-2510, Telegram Desktop
// apiwrap.cpp:3476), and all of them with no ids: Android sends the same
// empty request for either kind (MediaDataController.java:2472-2487), so
// it reads the trending sets of both. An id that is no trending set is
// left out.
func (c *CatalogCore) MessagesReadFeaturedStickers(in *mtproto.TLMessagesReadFeaturedStickers) (*mtproto.Bool, error) {
	const method = "messages.readFeaturedStickers"
	if c.MD.IsBot {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrBotMethodInvalid)
		return nil, mtproto.ErrBotMethodInvalid
	}
	if len(in.GetId()) > maxReadFeatured {
		c.Logger.Errorf("%s - %d ids: %v", method, len(in.GetId()), mtproto.ErrLimitInvalid)
		return nil, mtproto.ErrLimitInvalid
	}
	var read [2][]int64
	for i, m := range []string{"messages.getFeaturedStickers", "messages.getFeaturedEmojiStickers"} {
		answer := &mtproto.Messages_FeaturedStickers{}
		if _, _, err := c.stored(m, "", answer); err != nil {
			return nil, err
		}
		trending := make(map[int64]bool, len(answer.GetSets()))
		for _, s := range answer.GetSets() {
			trending[s.GetSet().GetId()] = true
		}
		if len(in.GetId()) == 0 {
			for _, s := range answer.GetSets() {
				read[i] = append(read[i], s.GetSet().GetId())
			}
			continue
		}
		for _, id := range in.GetId() {
			if trending[id] {
				read[i] = append(read[i], id)
				delete(trending, id)
			}
		}
	}
	if err := c.svcCtx.Installed.ReadFeatured(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, read[0], read[1]); err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// maxReadFeatured is the most sets one request may mark: this server's
// bound; a page of trending sets is far smaller.
const maxReadFeatured = 1000

// featuredFor makes a stored list of trending sets the account's: the sets
// it has not seen are its unread ones (messages.readFeaturedStickers), and
// the hash tells them too, so that a client whose list has other unread
// sets loads it again. A list all of which the account has seen keeps the
// stored hash.
func (c *CatalogCore) featuredFor(method string, answer *mtproto.Messages_FeaturedStickers, hash int64) (int64, error) {
	read, err := c.svcCtx.Installed.FeaturedRead(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("%s - the trending sets seen: %v", method, err)
		return 0, err
	}
	unread := []int64{}
	for _, s := range answer.GetSets() {
		if id := s.GetSet().GetId(); !read[id] {
			unread = append(unread, id)
		}
	}
	answer.Unread = unread
	if len(unread) == 0 {
		return hash, nil
	}
	h := uint64(hash)
	for _, id := range unread {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(id)
	}
	return int64(h), nil
}
