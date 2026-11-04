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

package channel

import (
	"github.com/teamgram/proto/mtproto"
)

// ToChannelParticipant is p's row as viewer sees it (channelParticipant*,
// core.telegram.org/type/ChannelParticipant): the creator, an admin (self
// for the viewer's own row), one banned or restricted, one who left, a
// member (channelParticipantSelf for the viewer, with who invited it).
func (p *ImmutableChannelParticipant) ToChannelParticipant(viewer int64) *mtproto.ChannelParticipant {
	userId := p.GetUserId()
	switch {
	case p.GetState() == StateLeft:
		return mtproto.MakeTLChannelParticipantLeft(&mtproto.ChannelParticipant{
			Peer: mtproto.MakePeerUser(userId),
		}).To_ChannelParticipant()
	case p.GetState() == StateBanned:
		return mtproto.MakeTLChannelParticipantBanned(&mtproto.ChannelParticipant{
			Peer:         mtproto.MakePeerUser(userId),
			KickedBy:     p.GetKickedBy(),
			Date:         int32(p.GetDate()),
			BannedRights: chatBannedRights(p.GetBannedRights(), p.GetUntilDate()),
		}).To_ChannelParticipant()
	case p.GetRole() == RoleCreator:
		return mtproto.MakeTLChannelParticipantCreator(&mtproto.ChannelParticipant{
			UserId:      userId,
			AdminRights: mtproto.AdminRights(p.GetAdminRights()).ToChatAdminRights(),
			Rank:        mtproto.MakeFlagsString(p.GetRank()),
		}).To_ChannelParticipant()
	case p.GetRole() == RoleAdmin:
		return mtproto.MakeTLChannelParticipantAdmin(&mtproto.ChannelParticipant{
			Self:                userId == viewer,
			UserId:              userId,
			InviterId_FLAGINT64: mtproto.MakeFlagsInt64(p.GetInviterUserId()),
			PromotedBy:          p.GetPromotedBy(),
			Date:                int32(p.GetDate()),
			AdminRights:         mtproto.AdminRights(p.GetAdminRights()).ToChatAdminRights(),
			Rank:                mtproto.MakeFlagsString(p.GetRank()),
		}).To_ChannelParticipant()
	case userId == viewer:
		return mtproto.MakeTLChannelParticipantSelf(&mtproto.ChannelParticipant{
			UserId:          userId,
			InviterId_INT64: p.GetInviterUserId(),
			Date:            int32(p.GetDate()),
		}).To_ChannelParticipant()
	default:
		return mtproto.MakeTLChannelParticipant(&mtproto.ChannelParticipant{
			UserId: userId,
			Date:   int32(p.GetDate()),
		}).To_ChannelParticipant()
	}
}

// chatBannedRights are the rights of a ban until untilDate; never nil, as
// channelParticipantBanned requires them.
func chatBannedRights(rights, untilDate int32) *mtproto.ChatBannedRights {
	r := mtproto.BannedRights(rights).ToChatBannedRights()
	if r == nil {
		r = mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{}).To_ChatBannedRights()
	}
	r.UntilDate = untilDate
	return r
}
