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
	"github.com/teamgram/teamgram-server/app/service/biz/message/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// MessageSearchByMediaType
// message.searchByMediaType user_id:long peer:PeerUtil media_type:int offset:int limit:int = Vector<MessageBox>;
//
// The messages of one media type of the dialog in the box of user_id,
// newest first, below the box id offset. A failed query is an error, not
// an empty list: messages.getRecentLocations would answer it with a hash
// the client keeps.
func (c *MessageCore) MessageSearchByMediaType(in *message.TLMessageSearchByMediaType) (*mtproto.MessageBoxList, error) {
	var (
		boxList []*mtproto.MessageBox
		err     error
	)

	switch in.GetMediaType() {
	case mtproto.MEDIA_PHONE_CALL:
		boxList, err = c.searchByPhoneCall(in.UserId, in.Offset, in.Limit)
	case mtproto.MEDIA_PHOTOVIDEO:
		boxList, err = c.searchByPhotoVideoMediaType(in.UserId, in.PeerType, in.PeerId, in.Offset, in.Limit)
	default:
		boxList, err = c.searchByMediaType(in.UserId, in.PeerType, in.PeerId, in.MediaType, in.Offset, in.Limit)
	}
	if err != nil {
		c.Logger.Errorf("message.searchByMediaType - error: %v", err)
		return nil, err
	}

	return mtproto.MakeTLMessageBoxList(&mtproto.MessageBoxList{
		BoxList: boxList,
	}).To_MessageBoxList(), nil
}

func (c *MessageCore) searchByPhotoVideoMediaType(
	userId int64,
	peerType int32,
	peerId int64,
	offset, limit int32) ([]*mtproto.MessageBox, error) {

	var (
		dialogId = mtproto.MakeDialogId(userId, peerType, peerId)
		boxList  = []*mtproto.MessageBox{}
	)

	_, err := c.svcCtx.Dao.MessagesDAO.SelectByPhotoVideoMediaTypeWithCB(
		c.ctx,
		userId,
		dialogId.A,
		dialogId.B,
		offset,
		limit,
		func(sz, i int, v *dataobject.MessagesDO) {
			boxList = append(boxList, c.svcCtx.Dao.MakeMessageBox(c.ctx, userId, v))
		})

	return boxList, err
}

func (c *MessageCore) searchByMediaType(
	userId int64,
	peerType int32,
	peerId int64,
	mediaType int32,
	offset, limit int32) ([]*mtproto.MessageBox, error) {

	var (
		dialogId = mtproto.MakeDialogId(userId, peerType, peerId)
		boxList  = []*mtproto.MessageBox{}
	)

	_, err := c.svcCtx.Dao.MessagesDAO.SelectByMediaTypeWithCB(
		c.ctx,
		userId,
		dialogId.A,
		dialogId.B,
		mediaType,
		offset,
		limit,
		func(sz, i int, v *dataobject.MessagesDO) {
			boxList = append(boxList, c.svcCtx.Dao.MakeMessageBox(c.ctx, userId, v))
		})

	return boxList, err
}

func (c *MessageCore) searchByPhoneCall(userId int64, offset, limit int32) ([]*mtproto.MessageBox, error) {
	boxList := []*mtproto.MessageBox{}
	_, err := c.svcCtx.Dao.MessagesDAO.SelectPhoneCallListWithCB(
		c.ctx,
		userId,
		mtproto.MEDIA_PHONE_CALL,
		offset,
		limit,
		func(sz, i int, v *dataobject.MessagesDO) {
			boxList = append(boxList, c.svcCtx.Dao.MakeMessageBox(c.ctx, userId, v))
		})

	return boxList, err
}
