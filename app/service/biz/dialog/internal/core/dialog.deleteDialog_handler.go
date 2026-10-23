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
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// DialogDeleteDialog
// dialog.deleteDialog user_id:long peer_type:int peer_id:long = Bool;
//
// The dialog row holds columns of the user's box (top_message,
// read_inbox_max_id, the unread counters): it is deleted in a box
// transaction (ADR 0011, p. 2), ordered with the box's other writes.
func (c *DialogCore) DialogDeleteDialog(in *dialog.TLDialogDeleteDialog) (*mtproto.Bool, error) {
	err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, in.PeerType, in.PeerId)},
		func(b *userbox.Box) error {
			n, err := c.svcCtx.Dao.DialogsDAO.DeleteTx(b.Tx(), in.UserId, in.PeerType, in.PeerId)
			if err != nil {
				return err
			}
			if n > 0 {
				b.InvalidateCache(dialog.DeleteCacheKeys(in.UserId, in.PeerType, in.PeerId)...)
			}
			return nil
		})
	if err != nil {
		c.Logger.Errorf("dialog.deleteDialog - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
