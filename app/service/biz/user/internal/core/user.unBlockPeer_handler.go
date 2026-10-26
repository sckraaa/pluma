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

// UserUnBlockPeer
// user.unBlockPeer user_id:long peer_type:int peer_id:long users:Vector<User> = Bool;
//
// The unblock and updatePeerBlocked with seq for every device of the
// account are one box transaction (ADR 0020); users are the peer, for the
// push. Unblocking a user not blocked changes nothing.
func (c *UserCore) UserUnBlockPeer(in *user.TLUserUnBlockPeer) (*mtproto.Bool, error) {
	if in.GetPeerType() != mtproto.PEER_USER || in.GetPeerId() <= 0 || in.GetPeerId() == in.GetUserId() {
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("user.unBlockPeer - error: %v", err)
		return nil, err
	}
	if err := c.svcCtx.Dao.UnBlockUser(c.ctx, in.GetUserId(), in.GetPeerId(), in.GetUsers()); err != nil {
		c.Logger.Errorf("user.unBlockPeer - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
