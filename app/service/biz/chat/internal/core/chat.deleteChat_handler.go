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

// ChatDeleteChat
// chat.deleteChat chat_id:long operator_id:long = MutableChat;
//
// The creator deletes the chat in its box transaction with the chat locked
// (ADR 0021): the chat is deactivated, every member removed. Operator 0 is
// the server's admin, who acts as the creator. A chat deleted already
// changes nothing and is returned again, so that bff can go on with the
// histories of its participants.
func (c *ChatCore) ChatDeleteChat(in *chat.TLChatDeleteChat) (*mtproto.MutableChat, error) {
	operatorId := in.OperatorId
	if operatorId == 0 {
		creator, err := c.creatorOf(in.ChatId)
		if err != nil {
			c.Logger.Errorf("chat.deleteChat - error: %v", err)
			return nil, err
		}
		operatorId = creator
	}

	chat2, err := c.changeChat(operatorId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		switch {
		case t.Chat.Creator != operatorId:
			return mtproto.ErrChatAdminRequired
		case t.Chat.Deactivated:
			return nil
		}
		d := t.c.svcCtx.Dao
		if _, err := d.ChatParticipantsDAO.UpdateStateByChatIdTx(b.Tx(), mtproto.ChatMemberStateKicked, in.ChatId); err != nil {
			return err
		}
		if err := d.DeleteChatTx(b, in.ChatId); err != nil {
			return err
		}
		for _, p := range t.ChatParticipants {
			if p.IsChatMemberStateNormal() {
				p.State = mtproto.ChatMemberStateKicked
			}
			t.changed(b, p.UserId)
		}
		t.Chat.ParticipantsCount = 0
		t.Chat.Deactivated = true
		t.Chat.Version++
		return nil
	})
	if err != nil {
		c.Logger.Errorf("chat.deleteChat - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
