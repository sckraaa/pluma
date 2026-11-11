// Copyright 2024 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsSetEmojiStickers
// channels.setEmojiStickers#3cd930b7 channel:InputChannel stickerset:InputStickerSet = Bool;
//
// The emoji set of a supergroup, from the catalog (pkg/channelsets); biz
// channel.setStickerSet checks the right and keeps the change. Boost levels
// do not apply (ADR 0041).
func (c *ChatsCore) ChannelsSetEmojiStickers(in *mtproto.TLChannelsSetEmojiStickers) (*mtproto.Bool, error) {
	channelId := in.GetChannel().GetChannelId()
	if channelId == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	set, err := c.svcCtx.Sets.Resolve(c.ctx, in.GetStickerset())
	if err != nil {
		return nil, err
	}
	var id, hash int64
	if set != nil {
		id, hash = set.ID, set.AccessHash
	}
	if _, err = c.svcCtx.Dao.Channel.ChannelSetStickerSet(c.ctx, &channelpb.TLChannelSetStickerSet{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     channelId,
		Emoji:         mtproto.BoolTrue,
		SetId:         id,
		AccessHash:    hash,
	}); err != nil {
		c.Logger.Errorf("channels.setEmojiStickers - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
