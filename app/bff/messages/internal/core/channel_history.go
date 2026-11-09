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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// channelHistory is messages.getHistory of a channel (task 039):
// messages.channelMessages with the channel's pts, which the clients seed
// its state with, the channel and the users of the page.
func (c *MessagesCore) channelHistory(channelId int64, in *mtproto.TLMessagesGetHistory, limit int32) (*mtproto.Messages_Messages, error) {
	r, err := c.svcCtx.Dao.Channel.ChannelGetHistory(c.ctx, &channelpb.TLChannelGetHistory{
		SelfId:     c.MD.UserId,
		ChannelId:  channelId,
		OffsetId:   in.OffsetId,
		OffsetDate: in.OffsetDate,
		AddOffset:  in.AddOffset,
		Limit:      limit,
		MaxId:      in.MaxId,
		MinId:      in.MinId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getHistory - error: %v", err)
		return nil, err
	}
	if err = c.fillChannelPeers(r, channelId); err != nil {
		c.Logger.Errorf("messages.getHistory - error: %v", err)
		return nil, err
	}
	return r, nil
}

// fillChannelPeers sets the chats and users of a page of a channel's
// messages: the channel, the channels and users the messages name, as the
// account sees them.
func (c *MessagesCore) fillChannelPeers(r *mtproto.Messages_Messages, channelId int64) error {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(r.GetMessages()...)
	// The clients read the creators of topics from users too: tdesktop
	// Data::ForumTopic::applyTopic (data_forum_topic.cpp) owner by from_id.
	for _, t := range r.GetTopics() {
		h.PickByPeer(t.GetFromId())
	}
	h.AppendChannels(channelId)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	channels, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: h.ChannelIdList})
	if err != nil {
		return err
	}
	r.Chats = channels.GetChannelListByIdList(c.MD.UserId)
	return nil
}
