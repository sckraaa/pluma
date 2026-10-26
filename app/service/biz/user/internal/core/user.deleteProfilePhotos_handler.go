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

// UserDeleteProfilePhotos
// user.deleteProfilePhotos user_id:long id:Vector<long> = Int64;
//
// Deletes photos of the account; when the main one is among them, the
// latest of the others becomes main. The change and updateUser with seq for
// every device of the account are one box transaction (ADR 0020). It
// returns the main photo now.
func (c *UserCore) UserDeleteProfilePhotos(in *user.TLUserDeleteProfilePhotos) (*mtproto.Int64, error) {
	mainId, err := c.svcCtx.Dao.DeleteProfilePhotos(c.ctx, in.GetUserId(), in.GetId())
	if err != nil {
		c.Logger.Errorf("user.deleteProfilePhotos - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return &mtproto.Int64{V: mainId}, nil
}
