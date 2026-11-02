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
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
)

// MessagesToggleStickerSets
// messages.toggleStickerSets#b5052fea flags:# uninstall:flags.0?true archive:flags.1?true unarchive:flags.2?true stickersets:Vector<InputStickerSet> = Bool;
//
// Uninstalls, archives or unarchives sets: exactly one of the flags, at
// most maxToggle sets.
func (c *CatalogCore) MessagesToggleStickerSets(in *mtproto.TLMessagesToggleStickerSets) (*mtproto.Bool, error) {
	var (
		action installed.Action
		flags  int
	)
	if in.GetUninstall() {
		action, flags = installed.Uninstall, flags+1
	}
	if in.GetArchive() {
		action, flags = installed.Archive, flags+1
	}
	if in.GetUnarchive() {
		action, flags = installed.Unarchive, flags+1
	}
	if flags != 1 || len(in.GetStickersets()) > maxToggle {
		return nil, mtproto.ErrInputRequestInvalid
	}
	if err := c.toggle("messages.toggleStickerSets", action, in.GetStickersets()); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// maxToggle bounds the sets of one toggleStickerSets.
const maxToggle = 200
