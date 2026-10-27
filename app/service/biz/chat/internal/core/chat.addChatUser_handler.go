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

// ChatAddChatUser
// chat.addChatUser chat_id:long inviter_id:long user_id:long = MutableChat;
//
// The inviter adds the user in its box transaction with the chat locked
// (ADR 0021); inviter 0 is the server's admin, who acts as the creator.
func (c *ChatCore) ChatAddChatUser(in *chat.TLChatAddChatUser) (*mtproto.MutableChat, error) {
	inviterId, checkInviter := in.InviterId, true
	if inviterId == 0 {
		creator, err := c.creatorOf(in.ChatId)
		if err != nil {
			c.Logger.Errorf("chat.addChatUser - error: %v", err)
			return nil, err
		}
		inviterId, checkInviter = creator, false
	}

	chat2, err := c.changeChat(inviterId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		return t.addMember(b, inviterId, in.UserId, in.GetIsBot(), checkInviter)
	})
	if err != nil {
		c.Logger.Errorf("chat.addChatUser - error: %v", err)
		return nil, err
	}
	return chat2, nil
}

// creatorOf returns the creator of chatId, for a change the server's admin
// makes in the creator's name.
func (c *ChatCore) creatorOf(chatId int64) (int64, error) {
	chat2, err := c.svcCtx.Dao.GetExcludeParticipantsMutableChat(c.ctx, chatId)
	if err != nil {
		return 0, err
	}
	return chat2.Creator(), nil
}
