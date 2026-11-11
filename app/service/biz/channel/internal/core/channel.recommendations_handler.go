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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
)

// ChannelGetChannelRecommendations
// channel.getChannelRecommendations self_id:long channel_id:long = messages.Chats;
//
// The public channels recommended to self_id (ADR 0047): those like
// channel_id, or with channel_id 0 those for the account, in their order,
// each as self_id sees it, a channel it is not in. The answer is
// messages.chats of all of them: Telegram's messages.chatsSlice tells a
// client without Premium how many more Premium would show (Android
// MessagesController.getChannelRecommendations, ChannelRecommendationsCell
// :148), and everybody has Premium here (ADR 0041), so none are held back.
func (c *ChannelCore) ChannelGetChannelRecommendations(in *channel.TLChannelGetChannelRecommendations) (*mtproto.Messages_Chats, error) {
	var (
		ids []int64
		err error
	)
	if in.ChannelId != 0 {
		ids, err = c.svcCtx.Dao.SimilarChannels(c.ctx, in.SelfId, in.ChannelId, dao.RecommendationsLimit)
	} else {
		ids, err = c.svcCtx.Dao.RecommendedChannels(c.ctx, in.SelfId, dao.RecommendationsLimit)
	}
	if err != nil {
		c.Logger.Errorf("channel.getChannelRecommendations - user %d, channel %d: %v", in.SelfId, in.ChannelId, err)
		return nil, err
	}
	channels, err := c.svcCtx.Dao.GetChannelListByIdList(c.ctx, in.SelfId, ids)
	if err != nil {
		c.Logger.Errorf("channel.getChannelRecommendations - user %d, channel %d: %v", in.SelfId, in.ChannelId, err)
		return nil, err
	}
	now := int32(time.Now().Unix())
	chats := make([]*mtproto.Chat, 0, len(channels))
	for _, mc := range channels {
		chats = append(chats, mc.ToChat(in.SelfId, now))
	}
	return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{Chats: chats}).To_Messages_Chats(), nil
}
