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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// DialogToggleDialogPin
// dialog.toggleDialogPin user_id:long peer_type:int peer_id:long pinned:Bool users:Vector<User> chats:Vector<Chat> = Int32;
//
// The pin and updateDialogPinned with seq for every device of the account
// are one box transaction (ADR 0020); users and chats are the peer for the
// push. It returns the folder of the dialog.
func (c *DialogCore) DialogToggleDialogPin(in *dialog.TLDialogToggleDialogPin) (*mtproto.Int32, error) {
	folderId, err := c.svcCtx.Dao.ToggleDialogPin(
		c.ctx,
		in.GetUserId(),
		in.GetPeerType(),
		in.GetPeerId(),
		mtproto.FromBool(in.GetPinned()),
		in.GetUsers(),
		in.GetChats())
	if err != nil {
		c.Logger.Errorf("dialog.toggleDialogPin - error: %v", err)
		return nil, err
	}

	return &mtproto.Int32{
		V: folderId,
	}, nil
}
