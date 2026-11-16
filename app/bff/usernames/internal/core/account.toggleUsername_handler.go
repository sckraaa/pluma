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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// The usernames of the account beside its editable one (task 104), as
// those of a channel (task 078): Telegram sells more on Fragment, which
// this server has not, so an account here has its editable username alone
// and these methods find nothing to change in it; they answer as Telegram
// does all the same, since the clients call them from the username screen
// (tdesktop api_user_names.cpp:174,227, Android
// ChangeUsernameActivity.java:290-310,613-640). biz user keeps the change
// and pushes updateUserName with seq to the account's devices and to those
// who see it. A bot's usernames are changed by its owner through
// bots.toggleUsername and bots.reorderUsernames, so a bot calling these is
// BOT_METHOD_INVALID.

// AccountToggleUsername
// account.toggleUsername#58d6b376 username:string active:Bool = Bool;
//
// Shows or hides one username of the account: USERNAME_INVALID for one
// that is not the account's, or for the editable one, which is always
// shown; USERNAME_NOT_MODIFIED for its state; USERNAMES_ACTIVE_TOO_MUCH
// past ten shown (core.telegram.org/method/account.toggleUsername).
func (c *UsernamesCore) AccountToggleUsername(in *mtproto.TLAccountToggleUsername) (*mtproto.Bool, error) {
	active := in.GetActive()
	if active == nil {
		active = mtproto.BoolFalse
	}
	return c.editAccountUsernames("account.toggleUsername", &userpb.TLUserEditUsernames{
		Action:   userpb.UsernamesToggle,
		Username: in.GetUsername(),
		Active:   active,
	})
}

// editAccountUsernames sends req, for the calling account, to biz user.
func (c *UsernamesCore) editAccountUsernames(method string, req *userpb.TLUserEditUsernames) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	req.UserId = c.MD.UserId
	r, err := c.svcCtx.Dao.UserClient.UserEditUsernames(c.ctx, req)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}
