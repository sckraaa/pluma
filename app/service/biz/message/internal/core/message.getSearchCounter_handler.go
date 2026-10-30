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
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// MessageGetSearchCounter
// message.getSearchCounter user_id:long peer_type:int peer_id:long media_type:int = Int32;
//
// Photos and videos are stored as types of their own; MEDIA_PHOTOVIDEO
// counts the messages searchByMediaType lists for it.
func (c *MessageCore) MessageGetSearchCounter(in *message.TLMessageGetSearchCounter) (*mtproto.Int32, error) {
	var (
		dialogId = mtproto.MakeDialogId(in.UserId, in.PeerType, in.PeerId)
	)

	if in.MediaType == mtproto.MEDIA_PHOTOVIDEO {
		n, err := c.svcCtx.Dao.MessagesDAO.CountByPhotoVideoMediaType(c.ctx, in.UserId, dialogId.A, dialogId.B)
		if err != nil {
			return nil, err
		}
		return &mtproto.Int32{V: n}, nil
	}

	sz := c.svcCtx.Dao.CommonDAO.CalcSize(
		c.ctx,
		c.svcCtx.Dao.MessagesDAO.CalcTableName(in.UserId),
		map[string]interface{}{
			"user_id":             in.UserId,
			"dialog_id1":          dialogId.A,
			"dialog_id2":          dialogId.B,
			"message_filter_type": in.MediaType,
			"deleted":             0,
		})

	return &mtproto.Int32{
		V: int32(sz),
	}, nil
}
