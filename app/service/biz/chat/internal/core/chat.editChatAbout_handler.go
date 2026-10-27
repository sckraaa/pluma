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

// ChatEditChatAbout
// chat.editChatAbout chat_id:long edit_user_id:long about:string = MutableChat;
//
// The description changes in the editor's box transaction with the chat
// locked (ADR 0021). It is a part of chatFull, which clients read again:
// the version of the chat stays.
func (c *ChatCore) ChatEditChatAbout(in *chat.TLChatEditChatAbout) (*mtproto.MutableChat, error) {
	chat2, err := c.changeChat(in.EditUserId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.EditUserId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case !me.CanChangeInfo():
			// 400	CHAT_ADMIN_REQUIRED	You must be an admin in this chat to do this
			return mtproto.ErrChatAdminRequired
		case t.Chat.About == in.About:
			return mtproto.ErrChatNotModified
		}
		if _, err := c.svcCtx.Dao.ChatsDAO.UpdateAboutTx(b.Tx(), in.About, in.ChatId); err != nil {
			return err
		}
		t.Chat.About = in.About
		return nil
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatAbout - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
