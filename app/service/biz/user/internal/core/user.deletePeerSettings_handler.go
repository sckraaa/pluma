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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserDeletePeerSettings
// user.deletePeerSettings user_id:int peer_type:int peer_id:int users:Vector<User> chats:Vector<Chat> = Bool;
//
// Hiding the settings (messages.hidePeerSettingsBar) and updatePeerSettings
// without a bar, with seq, for every device of the account are one box
// transaction (ADR 0020); settings hidden or never stored change nothing.
// users and chats are the peer for the push.
func (c *UserCore) UserDeletePeerSettings(in *user.TLUserDeletePeerSettings) (*mtproto.Bool, error) {
	err := c.svcCtx.Dao.DeleteUserPeerSettings(c.ctx, in.GetUserId(), in.GetPeerType(), in.GetPeerId(), in.GetUsers(), in.GetChats())
	if err != nil {
		c.Logger.Errorf("user.deletePeerSettings - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
