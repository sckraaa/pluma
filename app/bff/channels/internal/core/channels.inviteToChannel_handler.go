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

// ChannelsInviteToChannelC9E33D54
// channels.inviteToChannel#c9e33d54 channel:InputChannel users:Vector<InputUser> = messages.InvitedUsers;
//
// The account adds users to the channel (task 041). One whose privacy
// ("who can add me to groups and channels") keeps the account from adding
// them is left out and named in missing_invitees, as for basic groups
// (task 034); deleted accounts are left out.
func (c *ChannelsCore) ChannelsInviteToChannelC9E33D54(in *mtproto.TLChannelsInviteToChannelC9E33D54) (*mtproto.Messages_InvitedUsers, error) {
	return c.invite(in.Channel, in.Users)
}

// ChannelsInviteToChannel199F3A6C
// channels.inviteToChannel#199f3a6c channel:InputChannel users:Vector<InputUser> = Updates;
func (c *ChannelsCore) ChannelsInviteToChannel199F3A6C(in *mtproto.TLChannelsInviteToChannel199F3A6C) (*mtproto.Updates, error) {
	r, err := c.invite(in.Channel, in.Users)
	if err != nil {
		return nil, err
	}
	return r.GetUpdates(), nil
}

func (c *ChannelsCore) invite(input *mtproto.InputChannel, inputUsers []*mtproto.InputUser) (*mtproto.Messages_InvitedUsers, error) {
	mc, err := c.mutableChannel(input)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(inputUsers))
	for _, u := range inputUsers {
		p := mtproto.FromInputUser(c.MD.UserId, u)
		if p.PeerType != mtproto.PEER_USER && p.PeerType != mtproto.PEER_SELF {
			return nil, mtproto.ErrUserIdInvalid
		}
		ids = append(ids, p.PeerId)
	}
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      append([]int64{c.MD.UserId}, ids...),
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	if !ok {
		return nil, mtproto.ErrInputUserDeactivated
	}
	var (
		allowed []int64
		missing = []*mtproto.MissingInvitee{}
	)
	for _, id := range ids {
		u, ok := users.GetImmutableUser(id)
		switch {
		case !ok || u.Deleted():
		case id == c.MD.UserId:
		default:
			if m := userpb.MissingInvitee(me, u); m != nil {
				missing = append(missing, m)
				continue
			}
			allowed = append(allowed, id)
		}
	}
	r, err := c.svcCtx.Channel.ChannelInviteToChannel(c.ctx, &channelpb.TLChannelInviteToChannel{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		Id:            allowed,
	})
	if err != nil {
		return nil, err
	}
	if err = c.addUsers(r); err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesInvitedUsers(&mtproto.Messages_InvitedUsers{
		Updates:         r,
		MissingInvitees: missing,
	}).To_Messages_InvitedUsers(), nil
}
