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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsGetChannels
// channels.getChannels#a7f6bbb id:Vector<InputChannel> = messages.Chats;
//
// The channels asked for, as the account sees them (task 038); a wrong
// access_hash is CHANNEL_INVALID, a channel that does not exist is left out.
func (c *ChannelsCore) ChannelsGetChannels(in *mtproto.TLChannelsGetChannels) (*mtproto.Messages_Chats, error) {
	ids := make([]int64, 0, len(in.Id))
	hashes := map[int64]int64{}
	for _, it := range in.Id {
		if it.GetPredicateName() != mtproto.Predicate_inputChannel || it.GetChannelId() == 0 {
			return nil, mtproto.ErrChannelInvalid
		}
		ids = append(ids, it.GetChannelId())
		hashes[it.GetChannelId()] = it.GetAccessHash()
	}
	chats := []*mtproto.Chat{}
	if len(ids) > 0 {
		list, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: ids})
		if err != nil {
			c.Logger.Errorf("channels.getChannels - error: %v", err)
			return nil, err
		}
		now := int32(time.Now().Unix())
		for _, mc := range list.GetDatas() {
			if mc.GetChannel().GetAccessHash() != hashes[mc.GetChannel().GetId()] {
				return nil, mtproto.ErrChannelInvalid
			}
			chats = append(chats, mc.ToChat(c.MD.UserId, now))
		}
	}
	return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{Chats: chats}).To_Messages_Chats(), nil
}
