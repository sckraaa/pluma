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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ChannelsGetFutureCreatorAfterLeave
// channels.getFutureCreatorAfterLeave#a00918af channel:InputChannel = User;
//
// Who owns the supergroup or channel seven days after the account, its
// owner, leaves it (biz channel.getFutureCreator, the user
// channel.leaveChannel names): Android asks it before an owner leaves and
// shows the user (AlertsCreator.java:2921, showLeaveGroupWithFutureOwner).
// CHAT_ADMIN_REQUIRED for anyone but the owner. With nobody to take the
// channel the answer is PARTICIPANT_ID_INVALID, on which Android leaves at
// once, as on userEmpty (as messages.getFutureChatCreatorAfterLeave).
func (c *ChannelsCore) ChannelsGetFutureCreatorAfterLeave(in *mtproto.TLChannelsGetFutureCreatorAfterLeave) (*mtproto.User, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelGetFutureCreator(c.ctx, &channelpb.TLChannelGetFutureCreator{SelfId: c.MD.UserId, ChannelId: mc.GetChannel().GetId()})
	if err != nil {
		return nil, err
	}
	next := r.GetV()
	if next == 0 {
		return nil, mtproto.ErrParticipantIdInvalid
	}
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: []int64{c.MD.UserId, next}})
	if err != nil {
		c.Logger.Errorf("channels.getFutureCreatorAfterLeave - error: %v", err)
		return nil, err
	}
	return users.GetUnsafeUser(c.MD.UserId, next)
}
