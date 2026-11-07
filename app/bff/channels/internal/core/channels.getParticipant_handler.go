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

// ChannelsGetParticipant
// channels.getParticipant#a0ab6cc6 channel:InputChannel participant:InputPeer = channels.ChannelParticipant;
//
// One member's row (tasks 038, 041): iOS asks for its own with the full
// channel (UpdateCachedPeerData.swift:657-680) and after joining
// (JoinChannel.swift:70-81). Anyone who may read a supergroup may ask
// about any member; in a channel only its admins ask about others
// (CHAT_ADMIN_REQUIRED). One not in the channel (never joined or left) is
// USER_NOT_PARTICIPANT, and so is one kicked to all but admins; a
// restricted member is channelParticipantBanned to admins.
func (c *ChannelsCore) ChannelsGetParticipant(in *mtproto.TLChannelsGetParticipant) (*mtproto.Channels_ChannelParticipant, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Participant)
	if peer.PeerType == mtproto.PEER_SELF {
		peer = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
	}
	if peer.PeerType != mtproto.PEER_USER {
		return nil, mtproto.ErrPeerIdInvalid
	}
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	target := peer.PeerId
	if target != c.MD.UserId {
		if err = readable(mc, c.MD.UserId); err != nil {
			return nil, err
		}
		me, _ := mc.GetParticipant(c.MD.UserId)
		if mc.GetChannel().GetBroadcast() && (!me.Member() || me.GetRole() == channelpb.RoleMember) {
			return nil, mtproto.ErrChatAdminRequired
		}
		if mc, err = c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{
			ChannelId: mc.GetChannel().GetId(),
			Id:        []int64{c.MD.UserId, target},
		}); err != nil {
			return nil, err
		}
	}
	row, ok := mc.GetParticipant(target)
	me, _ := mc.GetParticipant(c.MD.UserId)
	switch {
	case !ok, row.GetState() == channelpb.StateLeft, target == c.MD.UserId && !row.Member():
		return nil, mtproto.ErrUserNotParticipant
	case row.GetState() == channelpb.StateKicked && !me.Admin():
		// Who was kicked is for admins (channelParticipantsKicked):
		// to a member one kicked is not in the channel.
		return nil, mtproto.ErrUserNotParticipant
	}
	now := int32(time.Now().Unix())
	p := row.ToChannelParticipant(me, now)
	h := mtproto.NewIDListHelper(c.MD.UserId)
	for _, id := range []int64{c.MD.UserId, target, row.GetInviterUserId(), row.GetPromotedBy(), row.GetKickedBy()} {
		if id != 0 {
			h.AppendUsers(id)
		}
	}
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLChannelsChannelParticipant(&mtproto.Channels_ChannelParticipant{
		Participant: p,
		Chats:       []*mtproto.Chat{mc.ToChat(c.MD.UserId, now)},
		Users:       users.GetUserListByIdList(c.MD.UserId, h.UserIdList...),
	}).To_Channels_ChannelParticipant(), nil
}
