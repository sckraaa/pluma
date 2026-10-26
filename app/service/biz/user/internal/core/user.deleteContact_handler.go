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
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// UserDeleteContact
// user.deleteContact user_id:long id:long users:Vector<User> = Bool;
//
// The removal of the contact and updatePeerSettings of it with seq for
// every device of the account are one box transaction of the user (ADR
// 0020); a user who is no contact changes nothing. users are the contact
// as the user sees it, for the push; the transaction makes it no contact.
func (c *UserCore) UserDeleteContact(in *user.TLUserDeleteContact) (*mtproto.Bool, error) {
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.GetUserId(), dao.ContactCacheKeys(in.GetUserId(), in.GetId()), func(b *userbox.Box) error {
		deleted, err := c.svcCtx.Dao.DeleteContact(b, in.GetId())
		if err != nil || !deleted {
			return err
		}
		users := dao.AsContact(in.GetUsers(), in.GetId(), nil, false)
		return c.svcCtx.Dao.EmitContactSettings(b, []int64{in.GetId()}, func(int64) []*mtproto.User { return users })
	})
	if err != nil {
		c.Logger.Errorf("user.deleteContact - user %d: %v", in.GetUserId(), err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
