/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatEditChatAdmin
// chat.editChatAdmin chat_id:long operator_id:long edit_chat_admin_id:long is_admin:Bool = MutableChat;
//
// A member who may add admins makes a member an admin or takes it back, in
// its box transaction with the chat locked (ADR 0021). A new admin gets its
// own permanent invite link. A member already in the asked role changes
// nothing.
func (c *ChatCore) ChatEditChatAdmin(in *chat.TLChatEditChatAdmin) (*mtproto.MutableChat, error) {
	isAdmin := mtproto.FromBool(in.IsAdmin)
	chat2, err := c.changeChat(in.OperatorId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.OperatorId)
		target := t.participant(in.EditChatAdminId)
		switch {
		case me == nil, target == nil:
			return mtproto.ErrUserNotParticipant
		case !target.IsChatMemberStateNormal():
			return mtproto.ErrPeerIdInvalid
		case !me.CanAdminAddAdmins():
			return mtproto.ErrChatAdminRequired
		case target.IsChatMemberCreator() && !isAdmin:
			return mtproto.ErrUserCreator
		case target.IsChatMemberCreator(), target.IsChatMemberAdmin() == isAdmin:
			return nil
		}

		d := t.c.svcCtx.Dao
		if !isAdmin {
			if _, err := d.ChatParticipantsDAO.UpdateParticipantTypeTx(b.Tx(), mtproto.ChatMemberNormal, target.Id); err != nil {
				return err
			}
			target.ParticipantType = mtproto.ChatMemberNormal
			target.AdminRights = nil
		} else {
			if _, err := d.ChatParticipantsDAO.UpdateParticipantTypeTx(b.Tx(), mtproto.ChatMemberAdmin, target.Id); err != nil {
				return err
			}
			if target.Link == "" {
				link := chat.GenChatInviteHash()
				if _, err := d.ChatParticipantsDAO.UpdateLinkTx(b.Tx(), link, in.ChatId, in.EditChatAdminId); err != nil {
					return err
				}
				if _, _, err := d.ChatInvitesDAO.InsertTx(b.Tx(), &dataobject.ChatInvitesDO{
					ChatId:    in.ChatId,
					AdminId:   in.EditChatAdminId,
					Link:      link,
					Permanent: true,
					Date2:     t.now,
				}); err != nil {
					return err
				}
				target.Link = link
			}
			target.ParticipantType = mtproto.ChatMemberAdmin
			target.AdminRights = mtproto.MakeDefaultChatAdminRights()
		}
		t.changed(b, in.EditChatAdminId)
		return t.bumpVersion(b)
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatAdmin - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
