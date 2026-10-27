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
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatDeleteChatUser
// chat.deleteChatUser chat_id:long operator_id:long delete_user_id:long = MutableChat;
//
// An admin removes a member, or a member leaves, in the operator's box
// transaction with the chat locked (ADR 0021); operator 0 is the server's
// admin, who acts as the creator. The removed user keeps the participant
// row, kicked or left. The members left get the members with seq, the
// removed user the forbidden list.
func (c *ChatCore) ChatDeleteChatUser(in *chat.TLChatDeleteChatUser) (*mtproto.MutableChat, error) {
	operatorId := in.OperatorId
	if operatorId == 0 {
		creator, err := c.creatorOf(in.ChatId)
		if err != nil {
			c.Logger.Errorf("chat.deleteChatUser - error: %v", err)
			return nil, err
		}
		operatorId = creator
	}
	kicked := operatorId != in.DeleteUserId

	change := c.changeChat
	if kicked {
		// The operator stays and gets the members.
		change = func(operatorId, chatId int64, fn func(b *userbox.Box, t *chatTx) error) (*mtproto.MutableChat, error) {
			return c.changeMembers(operatorId, chatId, nil, fn)
		}
	}
	chat2, err := change(operatorId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.participant(operatorId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case !me.IsChatMemberStateNormal():
			return mtproto.ErrPeerIdInvalid
		}
		d := t.c.svcCtx.Dao
		if kicked {
			removed := t.participant(in.DeleteUserId)
			switch {
			case !me.CanAdminBanUsers():
				return mtproto.ErrChatAdminRequired
			case removed == nil:
				return mtproto.ErrUserNotParticipant
			case !removed.IsChatMemberStateNormal():
				return mtproto.ErrPeerIdInvalid
			case removed.IsChatMemberCreator():
				// Only the creator leaves on its own.
				return mtproto.ErrChatAdminRequired
			}
			if _, err := d.ChatParticipantsDAO.UpdateKickedTx(b.Tx(), t.now, removed.Id); err != nil {
				return err
			}
			removed.State = mtproto.ChatMemberStateKicked
			removed.KickedAt = t.now
			removed.LeftAt = 0
		} else {
			if _, err := d.ChatParticipantsDAO.UpdateLeftTx(b.Tx(), t.now, me.Id); err != nil {
				return err
			}
			me.State = mtproto.ChatMemberStateLeft
			me.LeftAt = t.now
			me.KickedAt = 0
		}
		// A user who comes back joins anew: by a link, a request, an
		// invitation.
		if _, err := d.ChatInviteParticipantsDAO.DeleteTx(b.Tx(), in.ChatId, in.DeleteUserId); err != nil {
			return err
		}
		t.changed(b, in.DeleteUserId)
		if err := t.writeCount(b); err != nil {
			return err
		}
		if err := t.tellMembers(b, t.participantsUpdate()); err != nil {
			return err
		}
		return t.tell(b, []int64{in.DeleteUserId}, t.forbiddenUpdate())
	})
	if err != nil {
		c.Logger.Errorf("chat.deleteChatUser - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
