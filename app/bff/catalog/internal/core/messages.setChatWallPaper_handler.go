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

// MessagesSetChatWallPaper
// messages.setChatWallPaper#8ffacae1 flags:# for_both:flags.3?true revert:flags.4?true peer:InputPeer wallpaper:flags.0?InputWallPaper settings:flags.2?WallPaperSettings id:flags.1?int = Updates;
//
// The wallpaper of a private chat, for one side or for both, of a channel
// or a supergroup (core.telegram.org/api/wallpapers). A private chat has
// the other side's user, not this account's own Saved Messages; a basic group
// has no wallpaper in its full info (chatFull in the schema of this layer).
func (c *CatalogCore) MessagesSetChatWallPaper(in *mtproto.TLMessagesSetChatWallPaper) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	switch {
	case peer.PeerType == mtproto.PEER_CHANNEL:
		return c.setChannelWallpaper(peer.PeerId, in)
	case peer.PeerType == mtproto.PEER_USER && peer.PeerId != c.MD.UserId:
		return c.setPrivateWallpaper(peer, in)
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
}
