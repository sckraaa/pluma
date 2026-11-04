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
// The account's own participation (task 038), which iOS asks for with the
// full channel (UpdateCachedPeerData.swift:657-680); other members are task
// 041. A user who is not in the channel is USER_NOT_PARTICIPANT.
func (c *ChannelsCore) ChannelsGetParticipant(in *mtproto.TLChannelsGetParticipant) (*mtproto.Channels_ChannelParticipant, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Participant)
	if peer.PeerType == mtproto.PEER_SELF {
		peer = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
	}
	if peer.PeerType != mtproto.PEER_USER || peer.PeerId != c.MD.UserId {
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	me, ok := mc.GetParticipant(c.MD.UserId)
	if !ok || !me.Member() {
		return nil, mtproto.ErrUserNotParticipant
	}
	var p *mtproto.ChannelParticipant
	switch me.GetRole() {
	case channelpb.RoleCreator:
		p = mtproto.MakeTLChannelParticipantCreator(&mtproto.ChannelParticipant{
			UserId:      c.MD.UserId,
			AdminRights: mtproto.AdminRights(me.GetAdminRights()).ToChatAdminRights(),
			Rank:        mtproto.MakeFlagsString(me.GetRank()),
		}).To_ChannelParticipant()
	case channelpb.RoleAdmin:
		p = mtproto.MakeTLChannelParticipantAdmin(&mtproto.ChannelParticipant{
			Self:        true,
			UserId:      c.MD.UserId,
			PromotedBy:  me.GetPromotedBy(),
			Date:        int32(me.GetDate()),
			AdminRights: mtproto.AdminRights(me.GetAdminRights()).ToChatAdminRights(),
			Rank:        mtproto.MakeFlagsString(me.GetRank()),
		}).To_ChannelParticipant()
	default:
		p = mtproto.MakeTLChannelParticipantSelf(&mtproto.ChannelParticipant{
			UserId:          c.MD.UserId,
			InviterId_INT64: me.GetInviterUserId(),
			Date:            int32(me.GetDate()),
		}).To_ChannelParticipant()
	}
	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId}, To: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLChannelsChannelParticipant(&mtproto.Channels_ChannelParticipant{
		Participant: p,
		Chats:       []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))},
		Users:       users.GetUserListByIdList(c.MD.UserId, c.MD.UserId),
	}).To_Channels_ChannelParticipant(), nil
}
