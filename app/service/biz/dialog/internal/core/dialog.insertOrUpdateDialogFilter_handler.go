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

// DialogInsertOrUpdateDialogFilter
// dialog.insertOrUpdateDialogFilter user_id:long id:int dialog_filter:DialogFilter users:Vector<User> chats:Vector<Chat> = Bool;
//
// Creates folder id or changes it (dao.SaveDialogFilter): a changed folder
// keeps its place, a new one goes last; DIALOG_FILTERS_TOO_MUCH over the
// limit. The folder and updateDialogFilter with seq for every device of the
// account are one box transaction (ADR 0020); users and chats are the
// folder's peers for the push.
func (c *DialogCore) DialogInsertOrUpdateDialogFilter(in *dialog.TLDialogInsertOrUpdateDialogFilter) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.SaveDialogFilter(c.ctx, in.UserId, in.Id, in.GetDialogFilter(), in.GetUsers(), in.GetChats()); err != nil {
		c.Logger.Errorf("dialog.insertOrUpdateDialogFilter - user %d, folder %d: %v", in.UserId, in.Id, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
