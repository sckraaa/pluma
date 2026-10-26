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

// UserAddPeerSettings
// user.addPeerSettings user_id:int peer_type:int peer_id:int settings:PeerSettings users:Vector<User> chats:Vector<Chat> = Bool;
//
// The settings and updatePeerSettings with seq for every device of the
// account are one box transaction (ADR 0020); users and chats are the peer
// for the push.
func (c *UserCore) UserAddPeerSettings(in *user.TLUserAddPeerSettings) (*mtproto.Bool, error) {
	err := c.svcCtx.Dao.SetUserPeerSettings(
		c.ctx,
		in.GetUserId(),
		in.GetPeerType(),
		in.GetPeerId(),
		in.GetSettings(),
		in.GetUsers(),
		in.GetChats())

	if err != nil {
		c.Logger.Errorf("user.addPeerSettings - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
