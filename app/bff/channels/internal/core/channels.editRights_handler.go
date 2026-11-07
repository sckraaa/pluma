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

// ChannelsEditAdmin
// channels.editAdmin#9a98ad68 flags:# channel:InputChannel user_id:InputUser admin_rights:ChatAdminRights rank:flags.0?string = Updates;
// channels.editAdmin#d33c8902 channel:InputChannel user_id:InputUser admin_rights:ChatAdminRights rank:string = Updates;
//
// The account promotes, edits or demotes an admin (task 042, biz
// channel.editAdmin): without rank the newer layer keeps the admin's
// title. The user must exist (INPUT_USER_DEACTIVATED for a deleted one).
func (c *ChannelsCore) ChannelsEditAdmin(in *mtproto.TLChannelsEditAdmin) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	p := mtproto.FromInputUser(c.MD.UserId, in.UserId)
	if p.PeerType != mtproto.PEER_USER && p.PeerType != mtproto.PEER_SELF {
		return nil, mtproto.ErrUserIdInvalid
	}
	if p.PeerId != c.MD.UserId {
		users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: []int64{p.PeerId}})
		if err != nil {
			return nil, err
		}
		if u, ok := users.GetImmutableUser(p.PeerId); !ok || u.Deleted() {
			return nil, mtproto.ErrInputUserDeactivated
		}
	}
	rank, keep := in.GetRank_STRING(), false
	if in.Constructor != mtproto.CRC32_channels_editAdmin_d33c8902 {
		rank, keep = in.GetRank_FLAGSTRING().GetValue(), in.GetRank_FLAGSTRING() == nil
	}
	r, err := c.svcCtx.Channel.ChannelEditAdmin(c.ctx, &channelpb.TLChannelEditAdmin{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		UserId:        p.PeerId,
		AdminRights:   int32(mtproto.MakeChatAdminRightsHelper(in.AdminRights)),
		Rank:          rank,
		KeepRank:      mtproto.ToBool(keep),
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

// ChannelsEditBanned
// channels.editBanned#96e6cd81 channel:InputChannel participant:InputPeer banned_rights:ChatBannedRights = Updates;
//
// The account restricts, kicks or frees a user (task 042, biz
// channel.editBanned). Only users are participants here: a channel as a
// sender is PARTICIPANT_ID_INVALID. Deleted accounts may be kicked, as the
// clients clean groups of them.
func (c *ChannelsCore) ChannelsEditBanned(in *mtproto.TLChannelsEditBanned) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	p := mtproto.FromInputPeer2(c.MD.UserId, in.Participant)
	if p.PeerType != mtproto.PEER_USER && p.PeerType != mtproto.PEER_SELF {
		return nil, mtproto.ErrParticipantIdInvalid
	}
	r, err := c.svcCtx.Channel.ChannelEditBanned(c.ctx, &channelpb.TLChannelEditBanned{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		UserId:        p.PeerId,
		BannedRights:  bannedFlags(in.BannedRights),
		UntilDate:     in.GetBannedRights().GetUntilDate(),
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

// bannedFlags are the flags of chatBannedRights, without its until_date.
func bannedFlags(r *mtproto.ChatBannedRights) int32 {
	if r == nil {
		return 0
	}
	return int32(uint32(r.ToBannedRights()))
}
