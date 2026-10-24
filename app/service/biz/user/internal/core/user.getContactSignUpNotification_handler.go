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

// UserGetContactSignUpNotification
// user.getContactSignUpNotification user_id:long = Bool;
//
// Whether the user is notified when a contact signs up: true unless the
// user turned it off. (account.getContactSignUpNotification answers the
// opposite: whether they are silent.)
func (c *UserCore) UserGetContactSignUpNotification(in *user.TLUserGetContactSignUpNotification) (*mtproto.Bool, error) {
	silenced, err := c.svcCtx.Dao.SignUpNotificationsSilenced(c.ctx, []int64{in.UserId})
	if err != nil {
		c.Logger.Errorf("user.getContactSignUpNotification - error: %v", err)
		return nil, err
	}

	return mtproto.ToBool(!silenced[in.UserId]), nil
}
