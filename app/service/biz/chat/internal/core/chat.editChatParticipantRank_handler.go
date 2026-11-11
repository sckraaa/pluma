// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
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

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatEditChatParticipantRank
// chat.editChatParticipantRank self_id:long chat_id:long participant:long rank:string = MutableChat;
//
// The rank changes in the editor's box transaction with the chat locked
// (ADR 0021): a change of the participants, which bumps the version and
// gives every member the members with seq.
func (c *ChatCore) ChatEditChatParticipantRank(in *chat.TLChatEditChatParticipantRank) (*mtproto.MutableChat, error) {
	if err := channelperm.CheckRank(in.Rank); err != nil {
		return nil, err
	}
	chat2, err := c.changeMembers(in.SelfId, in.ChatId, nil, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.SelfId)
		participant := t.member(in.Participant)
		switch {
		case me == nil:
			return mtproto.ErrUserNotParticipant
		case participant == nil:
			return mtproto.ErrPeerIdInvalid
		case !canEditChatParticipantRank(me, participant, t.Chat.DefaultBannedRights):
			return mtproto.ErrChatAdminRequired
		case participant.Rank == in.Rank:
			return mtproto.ErrChatNotModified
		}
		if _, err := t.c.svcCtx.Dao.ChatParticipantsDAO.UpdateRankTx(b.Tx(), in.Rank, participant.Id); err != nil {
			return err
		}
		participant.Rank = in.Rank
		t.changed(b, in.Participant)
		if err := t.bumpVersion(b); err != nil {
			return err
		}
		return t.tellMembers(b, t.participantsUpdate())
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatParticipantRank - error: %v", err)
		return nil, err
	}
	return chat2, nil
}

// canEditChatParticipantRank is who may set a member tag, as the clients
// offer it (Android ProfileActivity.java:7270, ChatObject.canManageMyTag;
// tdesktop PeerData::canManageRanks): the creator anyone's; a member its own
// unless the group forbids it; an admin with manage_ranks its own, a member's
// and an admin's it promoted.
func canEditChatParticipantRank(operator, target *mtproto.ImmutableChatParticipant, defaultBanned *mtproto.ChatBannedRights) bool {
	if operator == nil || target == nil || operator.State != mtproto.ChatMemberStateNormal {
		return false
	}
	if operator.IsChatMemberCreator() {
		return true
	}
	manages := operator.IsChatMemberAdmin() && operator.AdminRights.GetManageRanks()
	if operator.UserId == target.UserId {
		return manages || !defaultBanned.GetEditRank()
	}
	if !manages || target.IsChatMemberCreator() {
		return false
	}
	return !target.IsChatMemberAdmin() || target.InviterUserId == operator.UserId
}
