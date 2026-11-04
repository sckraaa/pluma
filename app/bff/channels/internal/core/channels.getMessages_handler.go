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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// getMessagesMax is the most ids of one call (core.telegram.org/method/
// channels.getMessages).
const getMessagesMax = 200

// ChannelsGetMessages
// channels.getMessages#ad8c9a23 channel:InputChannel id:Vector<InputMessage> = messages.Messages;
//
// The messages of the channel asked by id (task 039): the clients fill
// replies and holes with it (iOS AccountStateManagementUtils.swift:2870,
// 2906); a missing one is left out. Ids only: replies to and pins of other
// messages come with 043.
func (c *ChannelsCore) ChannelsGetMessages(in *mtproto.TLChannelsGetMessages) (*mtproto.Messages_Messages, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if len(in.Id_VECTORINPUTMESSAGE) > getMessagesMax {
		return nil, mtproto.ErrLimitInvalid
	}
	ids := make([]int32, 0, len(in.Id_VECTORINPUTMESSAGE))
	for _, it := range in.Id_VECTORINPUTMESSAGE {
		if it.GetPredicateName() == mtproto.Predicate_inputMessageID {
			ids = append(ids, it.GetId())
		}
	}
	ids = append(ids, in.Id_VECTORINT32...)
	messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: mc.GetChannel().GetId(), Id: ids})
	if err != nil {
		c.Logger.Errorf("channels.getMessages - error: %v", err)
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(messages.GetDatas()...)
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesChannelMessages(&mtproto.Messages_Messages{
		Pts:      mc.GetChannel().GetPts(),
		Count:    int32(len(messages.GetDatas())),
		Messages: messages.GetDatas(),
		Topics:   []*mtproto.ForumTopic{},
		Chats:    []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))},
		Users:    users.GetUserListByIdList(c.MD.UserId, h.UserIdList...),
	}).To_Messages_Messages(), nil
}
