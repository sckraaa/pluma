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

// DialogMarkDialogUnread
// dialog.markDialogUnread user_id:long peer_type:int peer_id:long unread_mark:Bool users:Vector<User> chats:Vector<Chat> = Bool;
//
// Sets the unread mark of the user's dialog with the peer
// (dao.MarkDialogUnread); PEER_ID_INVALID when the user has no such dialog.
// The mark and updateDialogUnreadMark with seq for every device of the
// account are one box transaction (ADR 0020); users and chats are the peer
// for the push.
func (c *DialogCore) DialogMarkDialogUnread(in *dialog.TLDialogMarkDialogUnread) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.MarkDialogUnread(c.ctx, in.UserId, in.PeerType, in.PeerId, mtproto.FromBool(in.GetUnreadMark()), in.GetUsers(), in.GetChats()); err != nil {
		c.Logger.Errorf("dialog.markDialogUnread - user %d, peer (%d, %d): %v", in.UserId, in.PeerType, in.PeerId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
