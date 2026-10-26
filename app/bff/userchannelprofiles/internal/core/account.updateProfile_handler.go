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
	"strings"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountUpdateProfile
// account.updateProfile#78515775 flags:# first_name:flags.0?string last_name:flags.1?string about:flags.2?string = User;
func (c *UserChannelProfilesCore) AccountUpdateProfile(in *mtproto.TLAccountUpdateProfile) (*mtproto.User, error) {
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("account.updateProfile - error: %v", err)
		return nil, mtproto.ErrUserInvalid
	}

	// A name not passed stays: a client changing only about passes only
	// about.
	firstName, lastName := me.FirstName(), me.LastName()
	if in.GetFirstName() != nil {
		firstName = in.GetFirstName().GetValue()
		if strings.TrimSpace(firstName) == "" {
			err = mtproto.ErrFirstnameInvalid
			c.Logger.Errorf("account.updateProfile - error: %v", err)
			return nil, err
		}
	}
	if in.GetLastName() != nil {
		lastName = in.GetLastName().GetValue()
	}

	if in.GetAbout() != nil {
		//// About长度 < 128 并且可以为 empty
		if len(in.GetAbout().GetValue()) > 128 {
			err = mtproto.ErrAboutTooLong
			c.Logger.Errorf("account.updateProfile - error: %v", err)
			return nil, err
		}

		if in.GetAbout().GetValue() != me.About() {
			if _, err = c.svcCtx.Dao.UserClient.UserUpdateAbout(c.ctx, &userpb.TLUserUpdateAbout{
				UserId: c.MD.UserId,
				About:  in.GetAbout().GetValue(),
			}); err != nil {
				c.Logger.Errorf("account.updateProfile - error: %v", err)
			} else {
				me.SetAbout(in.GetAbout().GetValue())
			}
		}
	}

	if firstName != me.FirstName() || lastName != me.LastName() {
		// biz pushes updateUserName with seq to every device of the
		// account, this one included, in the transaction of the change
		// (ADR 0020).
		if _, err = c.svcCtx.Dao.UserClient.UserUpdateFirstAndLastName(c.ctx, &userpb.TLUserUpdateFirstAndLastName{
			UserId:    c.MD.UserId,
			FirstName: firstName,
			LastName:  lastName,
		}); err != nil {
			c.Logger.Errorf("account.updateProfile - error: %v", err)
			return nil, err
		}
		me.SetFirstName(firstName)
		me.SetLastName(lastName)
	}

	return me.ToSelfUser(), nil
}
