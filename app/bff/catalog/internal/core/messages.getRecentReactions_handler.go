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

// MessagesGetRecentReactions
// messages.getRecentReactions#39461db2 limit:int hash:long = messages.Reactions;
//
// The reactions the account used lately: none until stage 2 keeps reactions.
func (c *CatalogCore) MessagesGetRecentReactions(in *mtproto.TLMessagesGetRecentReactions) (*mtproto.Messages_Reactions, error) {
	return emptyReactions(), nil
}
