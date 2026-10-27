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

// ChatEditChatTitle
// chat.editChatTitle chat_id:long edit_user_id:long title:string = MutableChat;
//
// The title changes in the editor's box transaction with the chat locked
// (ADR 0021); the service message that bff sends tells the members.
func (c *ChatCore) ChatEditChatTitle(in *chat.TLChatEditChatTitle) (*mtproto.MutableChat, error) {
	if in.Title == "" {
		err := mtproto.ErrChatTitleEmpty
		c.Logger.Errorf("chat.editChatTitle - error: %v", err)
		return nil, err
	}

	chat2, err := c.changeChat(in.EditUserId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.EditUserId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case !me.CanChangeInfo():
			// 400	CHAT_ADMIN_REQUIRED	You must be an admin in this chat to do this
			return mtproto.ErrChatAdminRequired
		case t.Chat.Title == in.Title:
			return mtproto.ErrChatNotModified
		}
		if _, err := c.svcCtx.Dao.ChatsDAO.UpdateTitleTx(b.Tx(), in.Title, in.ChatId); err != nil {
			return err
		}
		t.Chat.Title = in.Title
		t.Chat.Version++
		return nil
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatTitle - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
