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

// ChannelsConvertToGigagroup
// channels.convertToGigagroup#b290c69 channel:InputChannel = Updates;
//
// The creator turns a large supergroup into a broadcast group, for good
// (biz channel.toggleSetting with channel.SettingGigagroup). The answer
// holds updateChannel and the channel with gigagroup, which the clients
// apply (Android MessagesController.convertToGigaGroup, tdesktop
// AboutGigagroupCallback, iOS convertGroupToGigagroup with a 5 s timeout);
// the members load the channel again on updateChannel.
func (c *ChatsCore) ChannelsConvertToGigagroup(in *mtproto.TLChannelsConvertToGigagroup) (*mtproto.Updates, error) {
	ch := in.GetChannel()
	if ch.GetPredicateName() != mtproto.Predicate_inputChannel || ch.GetChannelId() == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{
		ChannelId: ch.GetChannelId(),
		Id:        []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("channels.convertToGigagroup - error: %v", err)
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != ch.GetAccessHash() {
		return nil, mtproto.ErrChannelInvalid
	}
	if mc.GetChannel().GetDeleted() {
		return nil, mtproto.ErrChannelPrivate
	}
	r, err := c.svcCtx.Dao.Channel.ChannelToggleSetting(c.ctx, &channelpb.TLChannelToggleSetting{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     ch.GetChannelId(),
		Setting:       channelpb.SettingGigagroup,
		Enabled:       mtproto.BoolTrue,
	})
	if err != nil {
		c.Logger.Errorf("channels.convertToGigagroup - error: %v", err)
		return nil, err
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{c.MD.UserId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("channels.convertToGigagroup - error: %v", err)
		return nil, err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, c.MD.UserId)
	return r, nil
}
