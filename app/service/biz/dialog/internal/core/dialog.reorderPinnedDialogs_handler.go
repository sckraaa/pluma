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
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// DialogReorderPinnedDialogs
// dialog.reorderPinnedDialogs user_id:long force:Bool folder_id:int id_list:Vector<long> = Bool;
//
// One box transaction of the user: the pinned dialogs are read, then each
// row changes by its key, in key order.
func (c *DialogCore) DialogReorderPinnedDialogs(in *dialog.TLDialogReorderPinnedDialogs) (*mtproto.Bool, error) {
	var (
		userId  = in.GetUserId()
		force   = mtproto.FromBool(in.GetForce())
		idList  = in.GetIdList()
		column  = "pinned"
		folder  = "folder_id = 0"
		listKey = dialog.GetPinnedDialogIdListCacheKey(userId)
		keys    = make([]string, 0, len(idList))
	)
	if in.GetFolderId() != 0 {
		column, folder, listKey = "folder_pinned", "folder_id = 1", dialog.GetFolderPinnedDialogIdListCacheKey(userId)
	}
	for _, id := range idList {
		keys = append(keys, dialog.GetDialogCacheKey(userId, id))
	}

	err := c.svcCtx.Dao.RunBox(c.ctx, userId, keys, func(b *userbox.Box) error {
		values := make(map[int64]int64, len(idList))
		if force {
			var pinned []int64
			if err := b.Tx().QueryRowsPartial(&pinned,
				"SELECT peer_dialog_id FROM dialogs WHERE user_id = ? AND "+folder+" AND "+column+" > 0", userId); err != nil {
				return err
			}
			for _, id := range pinned {
				if !slices.Contains(idList, id) {
					values[id] = 0
				}
			}
		}
		order := time.Now().Unix()
		for _, id := range idList {
			values[id] = order << 32
			order--
		}
		ids := make([]int64, 0, len(values))
		for id := range values {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if _, err := b.Tx().Exec("UPDATE dialogs SET "+column+" = ? WHERE user_id = ? AND "+folder+" AND peer_dialog_id = ?",
				values[id], userId, id); err != nil {
				return err
			}
			b.InvalidateCache(dialog.GetDialogCacheKey(userId, id))
		}
		b.InvalidateCache(listKey)
		return nil
	})
	if err != nil {
		c.Logger.Errorf("dialog.reorderPinnedDialogs - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
