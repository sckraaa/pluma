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

// MessagesGetAvailableEffects
// messages.getAvailableEffects#dea20a39 hash:int = messages.AvailableEffects;
//
// The message effects and their animations. From the catalog; nothing while it is not imported.
func (c *CatalogCore) MessagesGetAvailableEffects(in *mtproto.TLMessagesGetAvailableEffects) (*mtproto.Messages_AvailableEffects, error) {
	answer := &mtproto.Messages_AvailableEffects{}
	hash, found, err := c.stored("messages.getAvailableEffects", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return mtproto.MakeTLMessagesAvailableEffects(&mtproto.Messages_AvailableEffects{Effects: []*mtproto.AvailableEffect{}, Documents: []*mtproto.Document{}}).To_Messages_AvailableEffects(), nil
	}
	if notModified(int64(in.GetHash()), hash) {
		return mtproto.MakeTLMessagesAvailableEffectsNotModified(nil).To_Messages_AvailableEffects(), nil
	}
	return answer, nil
}
