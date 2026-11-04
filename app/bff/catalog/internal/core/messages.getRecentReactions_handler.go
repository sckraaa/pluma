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
	"github.com/teamgram/teamgram-server/pkg/hashx"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesGetRecentReactions
// messages.getRecentReactions#39461db2 limit:int hash:long = messages.Reactions;
//
// The reactions the account put lately outside Saved Messages, the latest
// first, since it last cleared them (task 053).
func (c *CatalogCore) MessagesGetRecentReactions(in *mtproto.TLMessagesGetRecentReactions) (*mtproto.Messages_Reactions, error) {
	limit := min(max(in.Limit, 1), 100)
	keys, err := reactions.Recent(c.ctx, c.svcCtx.Reactions, c.MD.UserId, limit)
	if err != nil {
		c.Logger.Errorf("messages.getRecentReactions - error: %v", err)
		return nil, err
	}
	var hash int64
	list := make([]*mtproto.Reaction, 0, len(keys))
	for _, k := range keys {
		if r := reactions.FromKey(k); r != nil {
			list = append(list, r)
			for _, b := range []byte(k) {
				hash = hashx.CombineInt64Hash(hash, int64(b))
			}
		}
	}
	if notModified(in.Hash, hash) {
		return mtproto.MakeTLMessagesReactionsNotModified(nil).To_Messages_Reactions(), nil
	}
	return mtproto.MakeTLMessagesReactions(&mtproto.Messages_Reactions{Hash: hash, Reactions: list}).To_Messages_Reactions(), nil
}
