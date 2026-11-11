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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatEditChatCreator
// chat.editChatCreator self_id:long chat_id:long user_id:long = MutableChat;
//
// The creator hands the basic group to the member user_id, which tdesktop
// asks with messages.editChatCreator on the group itself (EditAdminBox::
// transferOwnership, channel_ownership_transfer.cpp:51): user_id becomes the
// creator, the old one an admin, and every member gets the members and the
// chat anew, in the creator's box transaction with the chat locked. The
// caller has checked the 2FA password. CHAT_ADMIN_REQUIRED for one not the
// creator, CHAT_NOT_MODIFIED for itself, USER_ID_INVALID for one not in it.
func (c *ChatCore) ChatEditChatCreator(in *chat.TLChatEditChatCreator) (*mtproto.MutableChat, error) {
	chat2, err := c.changeMembers(in.SelfId, in.ChatId, nil, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.SelfId)
		target := t.member(in.UserId)
		switch {
		case me == nil:
			return mtproto.ErrUserNotParticipant
		case !me.IsChatMemberCreator():
			return mtproto.ErrChatAdminRequired
		case in.UserId == in.SelfId:
			return mtproto.ErrChatNotModified
		case target == nil:
			return mtproto.ErrUserIdInvalid
		}
		if err := t.handTo(b, me, target, mtproto.ChatMemberAdmin); err != nil {
			return err
		}
		t.changed(b, in.SelfId, in.UserId)
		if err := t.bumpVersion(b); err != nil {
			return err
		}
		return t.tellMembers(b, t.participantsUpdate(), t.chatUpdate())
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatCreator - error: %v", err)
		return nil, err
	}
	return chat2, nil
}

// handTo makes target the creator of the chat, and me, its creator, a
// participant of type oldType: an admin when it hands the chat over
// (chat.editChatCreator), a member when it leaves (chat.deleteChatUser).
// The new creator gets the permanent link the creator has, as an admin.
func (t *chatTx) handTo(b *userbox.Box, me, target *mtproto.ImmutableChatParticipant, oldType int32) error {
	d := t.c.svcCtx.Dao
	if _, err := d.ChatParticipantsDAO.UpdateParticipantTypeTx(b.Tx(), mtproto.ChatMemberCreator, target.Id); err != nil {
		return err
	}
	if _, err := d.ChatParticipantsDAO.UpdateParticipantTypeTx(b.Tx(), oldType, me.Id); err != nil {
		return err
	}
	if _, err := b.Tx().Exec("UPDATE chats SET creator_user_id = ? WHERE id = ?", target.UserId, t.Chat.Id); err != nil {
		return err
	}
	if target.Link == "" {
		// The creator, as an admin, has its permanent link.
		link := chat.GenChatInviteHash()
		if _, err := d.ChatParticipantsDAO.UpdateLinkTx(b.Tx(), link, t.Chat.Id, target.UserId); err != nil {
			return err
		}
		if _, _, err := d.ChatInvitesDAO.InsertTx(b.Tx(), &dataobject.ChatInvitesDO{
			ChatId:    t.Chat.Id,
			AdminId:   target.UserId,
			Link:      link,
			Permanent: true,
			Date2:     t.now,
		}); err != nil {
			return err
		}
		target.Link = link
	}
	target.ParticipantType = mtproto.ChatMemberCreator
	me.ParticipantType = oldType
	me.AdminRights = nil
	if oldType == mtproto.ChatMemberAdmin {
		me.AdminRights = mtproto.MakeDefaultChatAdminRights()
	}
	t.Chat.Creator = target.UserId
	return nil
}
