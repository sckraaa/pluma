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

// importChannelInvite joins the account to the channel of the link (task
// 041): biz channel writes the join, the users of the answer are added
// here.
func (c *ChatInvitesCore) importChannelInvite(hash string) (*mtproto.Updates, error) {
	r, err := c.svcCtx.Dao.Channel.ChannelJoinChannel(c.ctx, &channelpb.TLChannelJoinChannel{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Link:          hash,
	})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendUsers(c.MD.UserId)
	h.PickByUpdates(r.GetUpdates()...)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	return r, nil
}
