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

// ChatEditChatDefaultBannedRights
// chat.editChatDefaultBannedRights chat_id:long operator_id:long banned_rights:ChatBannedRights = MutableChat;
//
// An admin changes the rights of the members, in its box transaction with
// the chat locked (ADR 0021). Rights without an end date are for good.
// Every member gets the rights with seq.
func (c *ChatCore) ChatEditChatDefaultBannedRights(in *chat.TLChatEditChatDefaultBannedRights) (*mtproto.MutableChat, error) {
	rights := in.BannedRights.ToBannedRights()
	chat2, err := c.changeChat(in.OperatorId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		me := t.member(in.OperatorId)
		switch {
		case me == nil:
			return mtproto.ErrInputUserDeactivated
		case me.IsChatMemberNormal():
			return mtproto.ErrChatAdminRequired
		case t.Chat.DefaultBannedRights.ToBannedRights() == rights:
			return mtproto.ErrChatNotModified
		}
		if _, err := c.svcCtx.Dao.ChatsDAO.UpdateDefaultBannedRightsTx(b.Tx(), int64(rights), in.ChatId); err != nil {
			return err
		}
		t.Chat.DefaultBannedRights = rights.ToChatBannedRights()
		t.Chat.Version++
		return t.tellMembers(b, mtproto.MakeTLUpdateChatDefaultBannedRights(&mtproto.Update{
			Peer_PEER:           mtproto.MakePeerChat(in.ChatId),
			DefaultBannedRights: t.Chat.DefaultBannedRights,
			Version:             t.Chat.Version,
		}).To_Update())
	})
	if err != nil {
		c.Logger.Errorf("chat.editChatDefaultBannedRights - error: %v", err)
		return nil, err
	}
	return chat2, nil
}
