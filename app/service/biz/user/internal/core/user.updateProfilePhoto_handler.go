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

// UserUpdateProfilePhoto
// user.updateProfilePhoto user_id:long id:long = Int64;
//
// Makes id the main photo of the account; id 0 removes the main photo and
// makes the latest of the others main. id is one of the account's photos,
// or one uploaded for it just now (uploaded); another is PHOTO_ID_INVALID.
// The change and updateUser with seq for every device of the account are
// one box transaction (ADR 0020). It returns the main photo now.
func (c *UserCore) UserUpdateProfilePhoto(in *user.TLUserUpdateProfilePhoto) (*mtproto.Int64, error) {
	mainId, err := c.svcCtx.Dao.UpdateProfilePhoto(c.ctx, in.GetUserId(), in.GetId(), in.GetUploaded())
	if err != nil {
		c.Logger.Errorf("user.updateProfilePhoto - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return &mtproto.Int64{V: mainId}, nil
}
