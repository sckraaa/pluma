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
	"time"
)

// ChatCheckChatInvite
// chat.checkChatInvite self_id:long hash:string = ChatInvite;
func (c *ChatCore) ChatCheckChatInvite(in *chat.TLChatCheckChatInvite) (*chat.ChatInviteExt, error) {
	chatInviteDO, err := c.svcCtx.Dao.ChatInvitesDAO.SelectByLink(c.ctx, in.Hash)
	if err != nil {
		c.Logger.Errorf("chat.checkChatInvite - error: %v", err)
		return nil, err
	} else if chatInviteDO == nil {
		c.Logger.Errorf("chat.checkChatInvite - error: invite hash not found")
		err = mtproto.ErrInviteHashInvalid
		return nil, err
	}
	// A link of a group that became a supergroup leads there: the caller
	// asks biz channel (task 067).
	if chatInviteDO.MigratedToId != 0 {
		return nil, mtproto.ErrMigratedToChannel
	}

	// check expire
	if chatInviteDO.Revoked {
		c.Logger.Errorf("chat.checkChatInvite - error: invite of chat %d revoked", chatInviteDO.ChatId)
		err = mtproto.ErrInviteHashExpired
		return nil, err
	}

	if chatInviteDO.ExpireDate != 0 && time.Now().Unix() > chatInviteDO.ExpireDate {
		c.Logger.Errorf("chat.checkChatInvite - error: invite of chat %d expired", chatInviteDO.ChatId)
		err = mtproto.ErrInviteHashExpired
		return nil, err
	}

	if chatInviteDO.UsageLimit > 0 {
		// TODO: calc
		sz := c.svcCtx.Dao.CommonDAO.CalcSize(
			c.ctx,
			"chat_invite_participants",
			map[string]interface{}{
				"link": chatInviteDO.Link,
			})
		if sz >= int(chatInviteDO.UsageLimit) {
			err = mtproto.ErrInviteHashExpired
			c.Logger.Errorf("chat.importChatInvite - error: %v", err)
			return nil, err
		}
	}

	mChat, err := c.svcCtx.Dao.GetMutableChat(c.ctx, chatInviteDO.ChatId)
	if err != nil {
		c.Logger.Errorf("chat.checkChatInvite - error: %v", err)
		return nil, err
	} else {
		if mChat.Deactivated() && mChat.GetChat().GetMigratedTo() != nil {
			err = mtproto.ErrMigratedToChannel
			c.Logger.Errorf("chat.checkChatInvite - error: %v", err)
			return nil, err
		}
	}

	// The link of an admin who may no longer invite is dead, as when it
	// is used (usable).
	if mayInvite(mChat, chatInviteDO.AdminId, time.Now().Unix()) != nil {
		err = mtproto.ErrInviteHashExpired
		c.Logger.Errorf("chat.importChatInvite - error: %v", err)
		return nil, err
	}

	me, _ := mChat.GetImmutableChatParticipant(in.SelfId)
	if me == nil || !me.IsChatMemberStateNormal() {
		rValue := chat.MakeTLChatInvite(&chat.ChatInviteExt{
			RequestNeeded:     chatInviteDO.RequestNeeded,
			Title:             mChat.Title(),
			About:             mtproto.MakeFlagsString(mChat.About()),
			Photo:             mChat.Photo(),
			ParticipantsCount: mChat.Chat.ParticipantsCount,
			Participants:      shownParticipants(mChat),
		}).To_ChatInviteExt()

		return rValue, nil
	} else {
		return chat.MakeTLChatInviteAlready(&chat.ChatInviteExt{
			Chat: mChat,
		}).To_ChatInviteExt(), nil
	}
}

// shownParticipants are the members of c the others see: the hidden ones
// (the ghost, ADR 0048) left out.
func shownParticipants(c *mtproto.MutableChat) []int64 {
	var ids []int64
	c.Walk(func(userId int64, p *mtproto.ImmutableChatParticipant) error {
		if p.IsChatMemberStateNormal() && !p.Hidden {
			ids = append(ids, userId)
		}
		return nil
	})
	return ids
}
