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

// UserSetNotifySettings
// user.setNotifySettings user_id:int peer_type:int peer_id:int settings:PeerNotifySettings users:Vector<User> chats:Vector<Chat> = Bool;
//
// The settings and updateNotifySettings with seq for every device of the
// account are one box transaction (ADR 0020); users and chats are the peer
// for the push.
func (c *UserCore) UserSetNotifySettings(in *user.TLUserSetNotifySettings) (*mtproto.Bool, error) {
	err := c.svcCtx.Dao.SetUserPeerNotifySettings(
		c.ctx,
		in.GetUserId(),
		in.GetPeerType(),
		in.GetPeerId(),
		in.GetSettings(),
		in.GetUsers(),
		in.GetChats())

	if err != nil {
		c.Logger.Errorf("user.setNotifySettings - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
