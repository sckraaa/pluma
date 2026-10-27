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

// UserUpdateUsername
// user.updateUsername user_id:long username:string = Bool;
//
// The username of the account ("" removes it) in users and in the username
// table, and updateUserName with seq for every device of the account, are
// one box transaction (ADR 0020). The same username changes nothing and
// answers false (account.updateUsername answers USERNAME_NOT_MODIFIED); one
// taken by another peer is USERNAME_OCCUPIED. The caller checks the format.
func (c *UserCore) UserUpdateUsername(in *user.TLUserUpdateUsername) (*mtproto.Bool, error) {
	changed, err := c.svcCtx.Dao.UpdateUsername(c.ctx, in.GetUserId(), in.GetUsername())
	if err != nil {
		c.Logger.Errorf("user.updateUsername - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.ToBool(changed), nil
}
