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

// AccountGetDefaultEmojiStatuses
// account.getDefaultEmojiStatuses#d6753386 hash:long = account.EmojiStatuses;
//
// The emoji statuses suggested to everyone. From the catalog; nothing while it is not imported.
func (c *CatalogCore) AccountGetDefaultEmojiStatuses(in *mtproto.TLAccountGetDefaultEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	answer := &mtproto.Account_EmojiStatuses{}
	hash, found, err := c.stored("account.getDefaultEmojiStatuses", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyEmojiStatuses(), nil
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLAccountEmojiStatusesNotModified(nil).To_Account_EmojiStatuses(), nil
	}
	return answer, nil
}
