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

// ChannelsToggleJoinRequest
// channels.toggleJoinRequest#4c2985b6 channel:InputChannel enabled:Bool = Updates;
// channels.toggleJoinRequest#ecc2618 flags:# apply_to_invites:flags.1?true channel:InputChannel enabled:Bool guard_bot:flags.0?InputUser = Updates;
//
// Whether those who join need an admin's approval (task 041, biz
// channel.toggleJoinRequest). A guard bot is a bot's and the server has no
// bots: inputUserEmpty clears it (nothing to do), any other is BOT_INVALID.
func (c *ChatInvitesCore) ChannelsToggleJoinRequest(in *mtproto.TLChannelsToggleJoinRequest) (*mtproto.Updates, error) {
	if in.GetChannel().GetPredicateName() != mtproto.Predicate_inputChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	if g := in.GetGuardBot(); g != nil && g.GetPredicateName() != mtproto.Predicate_inputUserEmpty {
		return nil, mtproto.ErrBotInvalid
	}
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.Channel.ChannelId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != in.Channel.AccessHash {
		return nil, mtproto.ErrChannelInvalid
	}
	return c.svcCtx.Dao.Channel.ChannelToggleJoinRequest(c.ctx, &channelpb.TLChannelToggleJoinRequest{
		SelfId:         c.MD.UserId,
		PermAuthKeyId:  c.MD.PermAuthKeyId,
		ChannelId:      in.Channel.ChannelId,
		Enabled:        in.Enabled,
		ApplyToInvites: mtproto.ToBool(in.ApplyToInvites),
	})
}
