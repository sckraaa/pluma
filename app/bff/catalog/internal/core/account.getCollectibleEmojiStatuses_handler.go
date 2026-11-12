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

// AccountGetCollectibleEmojiStatuses
// account.getCollectibleEmojiStatuses#2e7b4543 hash:long = account.EmojiStatuses;
//
// The statuses made of the collectible gifts an account owns
// (core.telegram.org/api/emoji-status). There are no gifts here, so nobody
// owns one: the list is empty for everyone. tdesktop and iOS ask by
// themselves when the picker of a status opens
// (data_emoji_statuses.cpp:435, ManagedRecentStickers.swift:334) and take
// an empty list as "none".
func (c *CatalogCore) AccountGetCollectibleEmojiStatuses(in *mtproto.TLAccountGetCollectibleEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	return emptyEmojiStatuses(), nil
}
