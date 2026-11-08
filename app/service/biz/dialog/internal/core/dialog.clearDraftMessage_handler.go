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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"

	"github.com/zeromicro/go-zero/core/jsonx"
)

func getEmptyDraftMessage() (string, error) {
	return jsonx.MarshalToString(
		mtproto.MakeTLDraftMessageEmpty(&mtproto.DraftMessage{
			Date_FLAGINT32: mtproto.MakeFlagsInt32(int32(time.Now().Unix())),
		}).To_DraftMessage())
}

// DialogClearDraftMessage
// dialog.clearDraftMessage user_id:long peer_type:int peer_id:long = Bool;
//
// It tells whether there was a draft to clear.
func (c *DialogCore) DialogClearDraftMessage(in *dialog.TLDialogClearDraftMessage) (*mtproto.Bool, error) {
	empty, err := getEmptyDraftMessage()
	if err != nil {
		c.Logger.Errorf("dialog.clearDraftMessage - error: %v", err)
		return nil, err
	}
	cleared, err := c.svcCtx.Dao.ClearDialogDraft(c.ctx, in.UserId, in.PeerType, in.PeerId, empty)
	if err != nil {
		c.Logger.Errorf("dialog.clearDraftMessage - error: %v", err)
		return nil, err
	}
	return mtproto.ToBool(cleared), nil
}
