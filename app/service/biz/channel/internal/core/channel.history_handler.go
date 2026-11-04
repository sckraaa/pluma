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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelGetMemberIdList
// channel.getMemberIdList channel_id:long = Vector<long>;
func (c *ChannelCore) ChannelGetMemberIdList(in *channel.TLChannelGetMemberIdList) (*channel.Vector_Long, error) {
	ids, err := c.svcCtx.Dao.MemberIds(c.ctx, in.ChannelId)
	if err != nil {
		c.Logger.Errorf("channel.getMemberIdList - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Long{Datas: ids}, nil
}

// ChannelGetHistory
// channel.getHistory self_id:long channel_id:long offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// messages.channelMessages with the channel's pts, which the clients seed
// the channel's state with (Android MessagesController.java:11892-11906,
// tdesktop data_session.cpp:2954-2957, Web K appMessagesManager.ts:
// 12531-12533; task 035).
func (c *ChannelCore) ChannelGetHistory(in *channel.TLChannelGetHistory) (*mtproto.Messages_Messages, error) {
	messages, count, err := c.svcCtx.Dao.History(c.ctx, in.SelfId, in.ChannelId, in.OffsetId, in.OffsetDate, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	if err != nil {
		c.Logger.Errorf("channel.getHistory - error: %v", err)
		return nil, err
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, nil)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesChannelMessages(&mtproto.Messages_Messages{
		Pts:      mc.GetChannel().GetPts(),
		Count:    count,
		Messages: messages,
		Topics:   []*mtproto.ForumTopic{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}).To_Messages_Messages(), nil
}

// ChannelGetMessages
// channel.getMessages self_id:long channel_id:long id:Vector<int> = Vector<Message>;
func (c *ChannelCore) ChannelGetMessages(in *channel.TLChannelGetMessages) (*channel.Vector_Message, error) {
	messages, err := c.svcCtx.Dao.Messages(c.ctx, in.SelfId, in.ChannelId, in.Id)
	if err != nil {
		c.Logger.Errorf("channel.getMessages - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Message{Datas: messages}, nil
}
