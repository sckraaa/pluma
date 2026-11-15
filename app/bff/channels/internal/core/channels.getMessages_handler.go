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
	"slices"
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
// 2906); a missing one is left out. inputMessagePinned is the channel's
// newest pinned message, the one channelFull.pinned_msg_id shows
// (channels.getFullChannel), and nothing when none is pinned; no client
// sends it (bots do). inputMessageReplyTo is not served yet.
func (c *ChannelsCore) ChannelsGetMessages(in *mtproto.TLChannelsGetMessages) (*mtproto.Messages_Messages, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if len(in.Id_VECTORINPUTMESSAGE) > getMessagesMax {
		return nil, mtproto.ErrLimitInvalid
	}
	ids := make([]int32, 0, len(in.Id_VECTORINPUTMESSAGE))
	pinned := false
	for _, it := range in.Id_VECTORINPUTMESSAGE {
		switch it.GetPredicateName() {
		case mtproto.Predicate_inputMessageID:
			ids = append(ids, it.GetId())
		case mtproto.Predicate_inputMessagePinned:
			pinned = true
		}
	}
	ids = append(ids, in.Id_VECTORINT32...)
	if pinned {
		id, err := c.newestPinned(mc.GetChannel().GetId())
		if err != nil {
			c.Logger.Errorf("channels.getMessages - error: %v", err)
			return nil, err
		}
		if id != 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
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

// newestPinned is the id of the newest message pinned in the channel as the
// user sees it, 0 for none: the query of channels.getFullChannel's
// pinned_msg_id (task 043).
func (c *ChannelsCore) newestPinned(channelId int64) (int32, error) {
	pinned, err := c.svcCtx.Channel.ChannelSearch(c.ctx, &channelpb.TLChannelSearch{
		SelfId: c.MD.UserId, ChannelId: channelId, MediaType: mtproto.MEDIA_PINNED, Limit: 1,
	})
	if err != nil {
		return 0, err
	}
	if m := pinned.GetMessages(); len(m) > 0 {
		return m[0].GetId(), nil
	}
	return 0, nil
}
