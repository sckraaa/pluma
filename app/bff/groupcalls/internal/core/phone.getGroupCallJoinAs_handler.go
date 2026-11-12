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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// PhoneGetGroupCallJoinAs
// phone.getGroupCallJoinAs#ef7c213a peer:InputPeer = phone.JoinAsPeers;
//
// The peers the account may join the calls of a chat as: the account
// alone, as joining as a channel is not offered (joinAs).
func (c *GroupCallsCore) PhoneGetGroupCallJoinAs(in *mtproto.TLPhoneGetGroupCallJoinAs) (*mtproto.Phone_JoinAsPeers, error) {
	a, err := c.loadInputPeer(in.GetPeer(), c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		return nil, mtproto.ErrChannelPrivate
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId}, To: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLPhoneJoinAsPeers(&mtproto.Phone_JoinAsPeers{
		Peers: []*mtproto.Peer{mtproto.MakePeerUser(c.MD.UserId)},
		Chats: []*mtproto.Chat{},
		Users: users.GetUserListByIdList(c.MD.UserId, c.MD.UserId),
	}).To_Phone_JoinAsPeers(), nil
}
