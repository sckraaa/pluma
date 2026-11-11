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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsSetStickers
// channels.setStickers#ea8ca4f9 channel:InputChannel stickerset:InputStickerSet = Bool;
//
// The sticker set of a supergroup, from the catalog (pkg/channelsets); biz
// channel.setStickerSet checks the right and keeps the change.
func (c *ChannelsCore) ChannelsSetStickers(in *mtproto.TLChannelsSetStickers) (*mtproto.Bool, error) {
	return c.setStickerSet(in.Channel, in.Stickerset, false)
}

// setStickerSet gives the supergroup the set asked, or none.
func (c *ChannelsCore) setStickerSet(inChannel *mtproto.InputChannel, ask *mtproto.InputStickerSet, emoji bool) (*mtproto.Bool, error) {
	mc, err := c.mutableChannel(inChannel)
	if err != nil {
		return nil, err
	}
	set, err := c.svcCtx.Sets.Resolve(c.ctx, ask)
	if err != nil {
		return nil, err
	}
	var id, hash int64
	if set != nil {
		id, hash = set.ID, set.AccessHash
	}
	if _, err = c.svcCtx.Channel.ChannelSetStickerSet(c.ctx, &channelpb.TLChannelSetStickerSet{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		Emoji:         mtproto.ToBool(emoji),
		SetId:         id,
		AccessHash:    hash,
	}); err != nil {
		c.Logger.Errorf("channels.setStickers - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
