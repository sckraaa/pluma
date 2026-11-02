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

// MessagesGetStickers
// messages.getStickers#d5a5d3a1 emoticon:string hash:long = messages.Stickers;
//
// The stickers of the account's sets for an emoji: none while it has no sets (stage 2).
func (c *CatalogCore) MessagesGetStickers(in *mtproto.TLMessagesGetStickers) (*mtproto.Messages_Stickers, error) {
	return mtproto.MakeTLMessagesStickers(&mtproto.Messages_Stickers{Stickers: []*mtproto.Document{}}).To_Messages_Stickers(), nil
}
