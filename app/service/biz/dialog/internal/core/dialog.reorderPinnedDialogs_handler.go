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

// DialogReorderPinnedDialogs
// dialog.reorderPinnedDialogs user_id:long force:Bool folder_id:int id_list:Vector<long> users:Vector<User> chats:Vector<Chat> = Bool;
//
// The order and updatePinnedDialogs with seq, with the order as stored, for
// every device of the account are one box transaction (ADR 0020); users and
// chats are the peers of id_list for the push.
func (c *DialogCore) DialogReorderPinnedDialogs(in *dialog.TLDialogReorderPinnedDialogs) (*mtproto.Bool, error) {
	err := c.svcCtx.Dao.ReorderPinnedDialogs(
		c.ctx,
		in.GetUserId(),
		mtproto.FromBool(in.GetForce()),
		in.GetFolderId(),
		in.GetIdList(),
		in.GetUsers(),
		in.GetChats())
	if err != nil {
		c.Logger.Errorf("dialog.reorderPinnedDialogs - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
