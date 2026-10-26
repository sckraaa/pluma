// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/marmota/pkg/strings2"
	"github.com/teamgram/marmota/pkg/utils"
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountUpdateUsername
// account.updateUsername#3e0bdd7c username:string = User;
func (c *UsernamesCore) AccountUpdateUsername(in *mtproto.TLAccountUpdateUsername) (*mtproto.User, error) {
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("account.updateUsername - error: %v", err)
		return nil, err
	}

	var (
		username2 = in.GetUsername()
	)

	if username2 != me.Username() {
		if err = checkAccountUsername(username2); err != nil {
			c.Logger.Errorf("account.updateUsername - error: %v", err)
			return nil, err
		}
		// biz replaces the username in users and in the username table and
		// pushes updateUserName with seq to every device of the account,
		// this one included, in one transaction (ADR 0020).
		if _, err = c.svcCtx.Dao.UserClient.UserUpdateUsername(c.ctx, &userpb.TLUserUpdateUsername{
			UserId:   c.MD.UserId,
			Username: username2,
		}); err != nil {
			c.Logger.Errorf("account.updateUsername - error: %v", err)
			return nil, err
		}
		me.SetUsername(username2)
	}

	return me.ToSelfUser(), nil
}

// checkAccountUsername checks the format of a username the account sets;
// "" removes it.
func checkAccountUsername(username string) error {
	if username == "" {
		return nil
	}
	if len(username) < userpb.MinUsernameLen ||
		!strings2.IsAlNumString(username) ||
		utils.IsNumber(username[0]) {
		return mtproto.ErrUsernameInvalid
	}
	return nil
}
