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

// UserAddSuggestedPhoto
// user.addSuggestedPhoto user_id:long from_user_id:long photo_id:long = Bool;
//
// Records the photo from_user_id suggested to the user, which the user may
// then make its main photo (task 051).
func (c *UserCore) UserAddSuggestedPhoto(in *user.TLUserAddSuggestedPhoto) (*mtproto.Bool, error) {
	if in.GetPhotoId() == 0 || in.GetFromUserId() == 0 || in.GetFromUserId() == in.GetUserId() {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("user.addSuggestedPhoto - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	if err := c.svcCtx.Dao.AddSuggestedPhoto(c.ctx, in.GetUserId(), in.GetFromUserId(), in.GetPhotoId()); err != nil {
		c.Logger.Errorf("user.addSuggestedPhoto - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
