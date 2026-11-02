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

// MessagesGetAvailableReactions
// messages.getAvailableReactions#18dea0ac hash:int = messages.AvailableReactions;
//
// The reactions every chat may use, with their animations. From the catalog; nothing while it is not imported.
func (c *CatalogCore) MessagesGetAvailableReactions(in *mtproto.TLMessagesGetAvailableReactions) (*mtproto.Messages_AvailableReactions, error) {
	answer := &mtproto.Messages_AvailableReactions{}
	hash, found, err := c.stored("messages.getAvailableReactions", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return mtproto.MakeTLMessagesAvailableReactions(&mtproto.Messages_AvailableReactions{Reactions: []*mtproto.AvailableReaction{}}).To_Messages_AvailableReactions(), nil
	}
	if notModified(int64(in.GetHash()), hash) {
		return mtproto.MakeTLMessagesAvailableReactionsNotModified(nil).To_Messages_AvailableReactions(), nil
	}
	return answer, nil
}
