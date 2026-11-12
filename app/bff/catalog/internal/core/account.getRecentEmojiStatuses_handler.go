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
	"github.com/teamgram/teamgram-server/pkg/appearance"
)

// AccountGetRecentEmojiStatuses
// account.getRecentEmojiStatuses#f578105 hash:long = account.EmojiStatuses;
//
// The statuses the account set lately, the newest first
// (pkg/emojistatus): each as a status without an end, as the picker sets
// it again with the end the user chooses. The hash is of the document ids
// in the order of the list (core.telegram.org/api/offsets#hash-generation).
func (c *CatalogCore) AccountGetRecentEmojiStatuses(in *mtproto.TLAccountGetRecentEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	recent, err := c.svcCtx.EmojiStatuses.Recent(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.getRecentEmojiStatuses - error: %v", err)
		return nil, err
	}
	var hash int64
	statuses := make([]*mtproto.EmojiStatus, 0, len(recent))
	for _, e := range recent {
		hash = appearance.CalcHash(hash, e.DocumentID)
		statuses = append(statuses, mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{DocumentId: e.DocumentID}).To_EmojiStatus())
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLAccountEmojiStatusesNotModified(nil).To_Account_EmojiStatuses(), nil
	}
	return mtproto.MakeTLAccountEmojiStatuses(&mtproto.Account_EmojiStatuses{
		Hash:     hash,
		Statuses: statuses,
	}).To_Account_EmojiStatuses(), nil
}
