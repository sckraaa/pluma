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

// MessagesGetDefaultTagReactions
// messages.getDefaultTagReactions#bdf93428 hash:long = messages.Reactions;
//
// The reactions suggested as tags of Saved Messages. From the catalog; nothing while it is not imported.
func (c *CatalogCore) MessagesGetDefaultTagReactions(in *mtproto.TLMessagesGetDefaultTagReactions) (*mtproto.Messages_Reactions, error) {
	answer := &mtproto.Messages_Reactions{}
	hash, found, err := c.stored("messages.getDefaultTagReactions", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyReactions(), nil
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLMessagesReactionsNotModified(nil).To_Messages_Reactions(), nil
	}
	return answer, nil
}
