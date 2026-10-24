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

// DialogUpdateDialogFiltersOrder
// dialog.updateDialogFiltersOrder user_id:long order:Vector<int> = Bool;
//
// Puts the folders in the order given; unknown ids are ignored and folders
// left out keep their relative order after the others
// (dao.OrderDialogFilters).
func (c *DialogCore) DialogUpdateDialogFiltersOrder(in *dialog.TLDialogUpdateDialogFiltersOrder) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.OrderDialogFilters(c.ctx, in.UserId, in.Order); err != nil {
		c.Logger.Errorf("dialog.updateDialogFiltersOrder - user %d: %v", in.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
