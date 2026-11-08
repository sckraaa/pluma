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
	"context"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// DialogClearAllDrafts
// dialog.clearAllDrafts user_id:long = Vector<PeerWithDraftMessage>;
//
// It clears every draft of the user, topics included, and returns what the
// other devices are to be told: an empty draft for each.
func (c *DialogCore) DialogClearAllDrafts(in *dialog.TLDialogClearAllDrafts) (*dialog.Vector_PeerWithDraftMessage, error) {
	drafts, err := c.DialogGetAllDrafts(&dialog.TLDialogGetAllDrafts{UserId: in.UserId})
	if err != nil {
		return nil, err
	}
	if len(drafts.Datas) == 0 {
		return drafts, nil
	}
	empty := int32(time.Now().Unix())
	keys := []string{dialog.GetAllDraftIdListCacheKey(in.UserId)}
	for _, v := range drafts.Datas {
		v.Draft = mtproto.MakeTLDraftMessageEmpty(&mtproto.DraftMessage{Date_FLAGINT32: mtproto.MakeFlagsInt32(empty)}).To_DraftMessage()
		if v.TopMsgId == 0 {
			p := mtproto.FromPeer(v.Peer)
			keys = append(keys, dialog.GetDialogCacheKeyByPeer(in.UserId, p.PeerType, p.PeerId))
		}
	}
	if _, _, err = c.svcCtx.Dao.CachedConn.Exec(c.ctx, func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
		n, err := c.svcCtx.Dao.DialogsDAO.ClearAllDrafts(ctx, in.UserId)
		return 0, n, err
	}, keys...); err != nil {
		c.Logger.Errorf("dialog.clearAllDrafts - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Dao.ClearThreadDrafts(c.ctx, in.UserId); err != nil {
		c.Logger.Errorf("dialog.clearAllDrafts - error: %v", err)
		return nil, err
	}
	return drafts, nil
}
