// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserSetContactPersonalPhoto
// user.setContactPersonalPhoto user_id:long contact_id:long photo_id:long has_video:Bool = User;
//
// Sets the personal photo the user sees for its contact, or with photo_id 0
// removes it; updateUser with seq goes to every device of the user in the
// transaction of the change (ADR 0020). It answers the contact as the user
// sees it now (task 051).
func (c *UserCore) UserSetContactPersonalPhoto(in *user.TLUserSetContactPersonalPhoto) (*mtproto.User, error) {
	seen, err := c.svcCtx.Dao.SetContactPersonalPhoto(c.ctx, in.GetUserId(), in.GetContactId(), in.GetPhotoId(),
		mtproto.FromBool(in.GetHasVideo()))
	if err != nil {
		c.Logger.Errorf("user.setContactPersonalPhoto - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return seen, nil
}
