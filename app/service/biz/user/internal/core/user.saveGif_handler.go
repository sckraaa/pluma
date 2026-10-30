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

// UserSaveGif
// user.saveGif user_id:long id:long unsave:Bool = Bool;
//
// The GIF goes to the top of the user's saved GIFs, or off them with
// unsave, and every device of the account gets updateSavedGifs with seq
// (task 020). bff checks that id is a GIF the user may save.
func (c *UserCore) UserSaveGif(in *user.TLUserSaveGif) (*mtproto.Bool, error) {
	if in.GetUserId() == 0 || in.GetId() == 0 {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("user.saveGif - user %d, gif %d: %v", in.GetUserId(), in.GetId(), err)
		return nil, err
	}
	if _, err := c.svcCtx.Dao.SaveGif(c.ctx, in.GetUserId(), in.GetId(), mtproto.FromBool(in.GetUnsave())); err != nil {
		c.Logger.Errorf("user.saveGif - user %d, gif %d: %v", in.GetUserId(), in.GetId(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
