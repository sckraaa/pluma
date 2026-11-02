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

// HelpGetPeerColors
// help.getPeerColors#da80f42f hash:int = help.PeerColors;
//
// The colors of names and replies. From the catalog; nothing while it is not imported.
func (c *CatalogCore) HelpGetPeerColors(in *mtproto.TLHelpGetPeerColors) (*mtproto.Help_PeerColors, error) {
	answer := &mtproto.Help_PeerColors{}
	hash, found, err := c.stored("help.getPeerColors", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return emptyPeerColors(), nil
	}
	if notModified(int64(in.GetHash()), hash) {
		return mtproto.MakeTLHelpPeerColorsNotModified(nil).To_Help_PeerColors(), nil
	}
	return answer, nil
}
