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

// UserUpdateFirstAndLastName
// user.updateFirstAndLastName user_id:long first_name:string last_name:string = Bool;
//
// The name and updateUserName with seq for every device of the account are
// one box transaction (ADR 0020); the same name changes nothing.
func (c *UserCore) UserUpdateFirstAndLastName(in *user.TLUserUpdateFirstAndLastName) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.UpdateFirstAndLastName(c.ctx, in.GetUserId(), in.GetFirstName(), in.GetLastName()); err != nil {
		c.Logger.Errorf("user.updateFirstAndLastName - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
