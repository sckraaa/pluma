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

// ChannelsGetChannelRecommendations
// channels.getChannelRecommendations#25a71742 flags:# channel:flags.0?InputChannel = messages.Chats;
//
// Similar channels (task 081, ADR 0047). With a channel, the channels like
// it, which all four clients ask for when they show a channel: Android
// MessagesController.getChannelRecommendations (MessagesController.java:
// 23901), iOS _internal_requestRecommendedChannels
// (ChannelRecommendation.swift:99), tdesktop ChatParticipants::loadSimilarPeers
// (api_chat_participants.cpp:860-875). Without one, the channels for the
// account, which the clients show in the search of chats (Android
// SearchViewPager.java:716, iOS ChatListSearchListPaneNode.swift:4245,
// tdesktop ChatParticipants::loadRecommendations, which names
// inputChannelEmpty with the flag off, api_chat_participants.cpp:927-930).
// The account must be able to read the channel: CHANNEL_PRIVATE otherwise,
// as for any other method about it.
func (c *ChannelsCore) ChannelsGetChannelRecommendations(in *mtproto.TLChannelsGetChannelRecommendations) (*mtproto.Messages_Chats, error) {
	var channelId int64
	if ch := in.GetChannel(); ch != nil && ch.GetPredicateName() != mtproto.Predicate_inputChannelEmpty {
		mc, err := c.mutableChannel(in.GetChannel())
		if err != nil {
			c.Logger.Errorf("channels.getChannelRecommendations - error: %v", err)
			return nil, err
		}
		if err = readable(mc, c.MD.UserId); err != nil {
			c.Logger.Errorf("channels.getChannelRecommendations - error: %v", err)
			return nil, err
		}
		channelId = mc.GetChannel().GetId()
	}
	r, err := c.svcCtx.Channel.ChannelGetChannelRecommendations(c.ctx, &channelpb.TLChannelGetChannelRecommendations{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
	})
	if err != nil {
		c.Logger.Errorf("channels.getChannelRecommendations - error: %v", err)
		return nil, err
	}
	return r, nil
}
