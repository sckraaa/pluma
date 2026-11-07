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
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// ToChannelParticipant is p's row as the viewer (its row: nil for one not
// in the channel) sees it at now (channelParticipant*,
// core.telegram.org/type/ChannelParticipant): the creator, an admin (self
// for the viewer's own row, can_edit when the viewer may edit it), one
// kicked (left) or restricted, one who left, a member
// (channelParticipantSelf for the viewer, with who invited it).
func (p *ImmutableChannelParticipant) ToChannelParticipant(viewer *ImmutableChannelParticipant, now int32) *mtproto.ChannelParticipant {
	userId := p.GetUserId()
	switch {
	case p.GetState() == StateKicked:
		return mtproto.MakeTLChannelParticipantBanned(&mtproto.ChannelParticipant{
			Left:         true,
			Peer:         mtproto.MakePeerUser(userId),
			KickedBy:     p.GetKickedBy(),
			Date:         int32(p.GetDate()),
			BannedRights: chatBannedRights(p.GetBannedRights(), p.GetUntilDate()),
		}).To_ChannelParticipant()
	case p.GetState() == StateLeft:
		return mtproto.MakeTLChannelParticipantLeft(&mtproto.ChannelParticipant{
			Peer: mtproto.MakePeerUser(userId),
		}).To_ChannelParticipant()
	case p.GetRole() == RoleCreator:
		return mtproto.MakeTLChannelParticipantCreator(&mtproto.ChannelParticipant{
			UserId:      userId,
			AdminRights: mtproto.AdminRights(channelperm.CreatorRights).ToChatAdminRights(),
			Rank:        mtproto.MakeFlagsString(p.GetRank()),
		}).To_ChannelParticipant()
	case p.GetRole() == RoleAdmin:
		return mtproto.MakeTLChannelParticipantAdmin(&mtproto.ChannelParticipant{
			CanEdit:             viewer.CanEditAdmin(p),
			Self:                userId == viewer.GetUserId(),
			UserId:              userId,
			InviterId_FLAGINT64: mtproto.MakeFlagsInt64(p.GetInviterUserId()),
			PromotedBy:          p.GetPromotedBy(),
			Date:                int32(p.GetDate()),
			AdminRights:         mtproto.AdminRights(p.GetAdminRights()).ToChatAdminRights(),
			Rank:                mtproto.MakeFlagsString(p.GetRank()),
		}).To_ChannelParticipant()
	case p.Restricted(now) && userId != viewer.GetUserId() && viewer.Admin():
		// A restriction is shown to admins; a member sees a restricted one
		// as a member, and its own restriction in the channel's
		// banned_rights (tdesktop takes channelParticipantBanned for its
		// own row as an API error, api_chat_participants.cpp:711-751).
		return mtproto.MakeTLChannelParticipantBanned(&mtproto.ChannelParticipant{
			Peer:         mtproto.MakePeerUser(userId),
			KickedBy:     p.GetKickedBy(),
			Date:         int32(p.GetDate()),
			BannedRights: chatBannedRights(p.GetBannedRights(), p.GetUntilDate()),
		}).To_ChannelParticipant()
	case userId == viewer.GetUserId():
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

// CanEditAdmin reports whether the viewer p may change the rights of the
// admin a: the creator may, and an admin with add_admins may change the
// admins it promoted (channelParticipantAdmin can_edit; iOS
// ChannelAdminController.swift:647-662, Web K canEditAdmin.ts:4-10).
func (p *ImmutableChannelParticipant) CanEditAdmin(a *ImmutableChannelParticipant) bool {
	if !p.Member() || a.GetRole() != RoleAdmin {
		return false
	}
	switch p.GetRole() {
	case RoleCreator:
		return true
	case RoleAdmin:
		return p.GetAdminRights()&mtproto.ADMIN_ADD_ADMINS != 0 && a.GetPromotedBy() == p.GetUserId()
	}
	return false
}

// Admin reports whether p is the creator or an admin in the channel.
func (p *ImmutableChannelParticipant) Admin() bool {
	return p.Member() && (p.GetRole() == RoleAdmin || p.GetRole() == RoleCreator)
}
