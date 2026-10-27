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

// DialogDeleteDialog
// dialog.deleteDialog user_id:long peer_type:int peer_id:long = Bool;
//
// The dialog row goes in a box transaction of the user (ADR 0011, p. 2),
// with updateDialogPinned with seq for the account's devices when the
// dialog was pinned (ADR 0020).
func (c *DialogCore) DialogDeleteDialog(in *dialog.TLDialogDeleteDialog) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.DeleteDialog(c.ctx, in.UserId, in.PeerType, in.PeerId); err != nil {
		c.Logger.Errorf("dialog.deleteDialog - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
