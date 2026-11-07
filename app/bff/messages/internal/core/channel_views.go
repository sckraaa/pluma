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

// channelMessagesViews is messages.getMessagesViews in a channel: for each
// id, in order, the messageViews of the message as the account sees it,
// with the comments of a post (replies), which the clients poll to keep the
// counts under the posts current (Android MessagesController.java:
// 10579-10630, task 035); an empty one for an id it may not see. Views and
// forwards are not counted yet (task 046).
func (c *MessagesCore) channelMessagesViews(channelId int64, ids []int32) (*mtproto.Messages_MessageViews, error) {
	messages, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: channelId, Id: ids})
	if err != nil {
		c.Logger.Errorf("messages.getMessagesViews - error: %v", err)
		return nil, err
	}
	byId := map[int32]*mtproto.Message{}
	for _, m := range messages.GetDatas() {
		byId[m.GetId()] = m
	}
	out := &mtproto.Messages_MessageViews{Views: make([]*mtproto.MessageViews, 0, len(ids))}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendChannels(channelId)
	for _, id := range ids {
		v := &mtproto.MessageViews{}
		if r := byId[id].GetReplies(); r != nil {
			v.Replies = r
			for _, p := range r.GetRecentRepliers() {
				h.AppendUsers(p.GetUserId())
			}
			if g := r.GetChannelId().GetValue(); g != 0 {
				h.AppendChannels(g)
			}
		}
		out.Views = append(out.Views, mtproto.MakeTLMessageViews(v).To_MessageViews())
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id: h.UserIdList, Privacy: true, HasTo: true, To: []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	out.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	channels, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: h.ChannelIdList})
	if err != nil {
		return nil, err
	}
	out.Chats = channels.GetChannelListByIdList(c.MD.UserId)
	return mtproto.MakeTLMessagesMessageViews(out).To_Messages_MessageViews(), nil
}
