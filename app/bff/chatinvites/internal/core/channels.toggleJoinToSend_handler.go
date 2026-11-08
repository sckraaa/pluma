// Copyright 2022 Teamgram Authors
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

// ChannelsToggleJoinToSend
// channels.toggleJoinToSend#e4cb9580 channel:InputChannel enabled:Bool = Updates;
//
// Whether the commenters of a discussion group join it first (task 044, biz
// channel.toggleSetting): off, the channel's subscribers comment without
// joining (task 048).
func (c *ChatInvitesCore) ChannelsToggleJoinToSend(in *mtproto.TLChannelsToggleJoinToSend) (*mtproto.Updates, error) {
	if in.GetChannel().GetPredicateName() != mtproto.Predicate_inputChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.Channel.ChannelId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != in.Channel.AccessHash {
		return nil, mtproto.ErrChannelInvalid
	}
	return c.svcCtx.Dao.Channel.ChannelToggleSetting(c.ctx, &channelpb.TLChannelToggleSetting{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     in.Channel.ChannelId,
		Setting:       channelpb.SettingJoinToSend,
		Enabled:       in.Enabled,
	})
}
