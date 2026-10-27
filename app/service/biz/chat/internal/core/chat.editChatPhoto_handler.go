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

// ChatEditChatPhoto
// chat.editChatPhoto chat_id:long edit_user_id:long chat_photo:Photo = MutableChat;
//
// The photo changes in the editor's box transaction with the chat locked
// (ADR 0021); an empty photo removes it. The service message that bff
// sends tells the members.
func (c *ChatCore) ChatEditChatPhoto(in *chat.TLChatEditChatPhoto) (*mtproto.MutableChat, error) {
	photoId := in.GetChatPhoto().GetId()
	chat2, err := c.changeChat(in.EditUserId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.EditUserId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case !me.CanChangeInfo():
			// 400	CHAT_ADMIN_REQUIRED	You must be an admin in this chat to do this
			return mtproto.ErrChatAdminRequired
		case t.PhotoId == photoId:
			return mtproto.ErrChatNotModified
		}
		if _, err := c.svcCtx.Dao.ChatsDAO.UpdatePhotoIdTx(b.Tx(), photoId, in.ChatId); err != nil {
			return err
		}
		t.PhotoId = photoId
		t.Chat.Photo = in.GetChatPhoto()
		t.Chat.Version++
		return nil
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatPhoto - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
