// Copyright 2025 Teamgram Authors
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

// ChannelsGetMessageAuthor
// channels.getMessageAuthor#ece2a0e6 channel:InputChannel id:int = User;
//
// Which admin wrote, as the channel, the message id of the monoforum
// channel (task 084; core.telegram.org/api/monoforum: "Monoforum admins ...
// can invoke channels.getMessageAuthor to get the original sender of a
// message sent by other monoforum admins"). The clients ask it for the
// messages of a monoforum that come from the channel (tdesktop
// history_item.cpp:2345-2350, Android ChatActivity.java:31965), and show
// the user it answers. CHANNEL_INVALID for a channel that is no monoforum.
func (c *SavedMessageDialogsCore) ChannelsGetMessageAuthor(in *mtproto.TLChannelsGetMessageAuthor) (*mtproto.User, error) {
	channelId := in.GetChannel().GetChannelId()
	if in.GetChannel().GetPredicateName() != mtproto.Predicate_inputChannel || channelId == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	author, err := c.svcCtx.Dao.Channel.ChannelGetMessageAuthor(c.ctx, &channelpb.TLChannelGetMessageAuthor{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
		Id:        in.GetId(),
	})
	if err != nil {
		c.Logger.Errorf("channels.getMessageAuthor - message %d of %d: %v", in.GetId(), channelId, err)
		return nil, err
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId, author.GetV()}})
	if err != nil {
		c.Logger.Errorf("channels.getMessageAuthor - error: %v", err)
		return nil, err
	}
	list := users.GetUserListByIdList(c.MD.UserId, author.GetV())
	if len(list) == 0 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	return list[0], nil
}
