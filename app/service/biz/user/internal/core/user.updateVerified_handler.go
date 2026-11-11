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

// UserUpdateVerified
// user.updateVerified user_id:long verified:Bool = Bool;
//
// Sets or clears the verification badge of the account (pluma-admin user
// verify). The change and updateUser to the account's devices and to the
// users who see it are one box transaction (Dao.UpdateVerified). It returns
// whether the flag changed; an unknown or deleted account is
// USER_ID_INVALID.
func (c *UserCore) UserUpdateVerified(in *user.TLUserUpdateVerified) (*mtproto.Bool, error) {
	changed, err := c.svcCtx.Dao.UpdateVerified(c.ctx, in.GetUserId(), mtproto.FromBool(in.GetVerified()))
	if err != nil {
		c.Logger.Errorf("user.updateVerified - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.ToBool(changed), nil
}
