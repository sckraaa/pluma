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

// ChannelsGetAdminedPublicChannels
// channels.getAdminedPublicChannels#f8b036af flags:# by_location:flags.0?true check_limit:flags.1?true for_personal:flags.2?true = messages.Chats;
//
// The public channels the account created (task 044): Android lists them
// when the account reaches the limit of public channels, to free the link
// of one, and reads messages.chats, not a slice (ChannelCreateActivity.java:
// 1201-1258 at aac1efcb). Geo groups (by_location) do not exist here (ADR
// 0050): none, and with check_limit CHANNELS_ADMIN_LOCATED_TOO_MUCH, the
// limit of geo groups being 0. check_limit answers
// CHANNELS_ADMIN_PUBLIC_TOO_MUCH at the limit, as Telegram
// (core.telegram.org/method/channels.getAdminedPublicChannels).
func (c *ChannelsCore) ChannelsGetAdminedPublicChannels(in *mtproto.TLChannelsGetAdminedPublicChannels) (*mtproto.Messages_Chats, error) {
	chats := []*mtproto.Chat{}
	if in.ByLocation {
		if in.CheckLimit {
			return nil, mtproto.ErrChannelsAdminLocatedTooMuch
		}
		return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{Chats: chats}).To_Messages_Chats(), nil
	}
	if in.CheckLimit {
		if _, err := c.svcCtx.Channel.ChannelCheckUsername(c.ctx, &channelpb.TLChannelCheckUsername{SelfId: c.MD.UserId}); err != nil {
			return nil, err
		}
	}
	ids, err := c.svcCtx.Channel.ChannelGetAdminedPublicChannels(c.ctx, &channelpb.TLChannelGetAdminedPublicChannels{SelfId: c.MD.UserId})
	if err != nil {
		return nil, err
	}
	if len(ids.GetDatas()) > 0 {
		list, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: ids.GetDatas()})
		if err != nil {
			return nil, err
		}
		chats = list.GetChannelListByIdList(c.MD.UserId)
	}
	return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{Chats: chats}).To_Messages_Chats(), nil
}
