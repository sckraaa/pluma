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
)

// ChatImportChatInvite
// chat.importChatInvite self_id:long hash:string = MutableChat;
//
// chat.importChatInvite2 that answers a request to join with
// INVITE_REQUEST_SENT.
func (c *ChatCore) ChatImportChatInvite(in *chat.TLChatImportChatInvite) (*mtproto.MutableChat, error) {
	imported, err := c.ChatImportChatInvite2(&chat.TLChatImportChatInvite2{
		SelfId: in.SelfId,
		Hash:   in.Hash,
	})
	if err != nil {
		return nil, err
	}
	if imported.GetRequesters() != nil {
		return nil, mtproto.ErrInviteRequestSent
	}
	return imported.GetChat(), nil
}
