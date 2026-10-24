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

// DialogDeleteDialogFilter
// dialog.deleteDialogFilter user_id:long id:int = Bool;
func (c *DialogCore) DialogDeleteDialogFilter(in *dialog.TLDialogDeleteDialogFilter) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.DeleteDialogFilter(c.ctx, in.UserId, in.Id); err != nil {
		c.Logger.Errorf("dialog.deleteDialogFilter - user %d, folder %d: %v", in.UserId, in.Id, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
