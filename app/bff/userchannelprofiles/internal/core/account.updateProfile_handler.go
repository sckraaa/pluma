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
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// AccountUpdateProfile
// account.updateProfile#78515775 flags:# first_name:flags.0?string last_name:flags.1?string about:flags.2?string = User;
//
// A connected business bot edits the name of its account with edit_name
// and the bio with edit_bio (task 105, core.telegram.org/constructor/
// businessBotRights), and gets the account as the bot sees it.
func (c *UserChannelProfilesCore) AccountUpdateProfile(in *mtproto.TLAccountUpdateProfile) (*mtproto.User, error) {
	var act *business.Act
	if id := c.MD.GetBusinessConnectionId(); id != "" {
		var err error
		if act, err = c.svcCtx.Dao.Business.Act(c.ctx, c.svcCtx.Dao.UserClient, c.MD.UserId, id, business.MethodUpdateProfile, nil, time.Now()); err != nil {
			c.Logger.Errorf("account.updateProfile - business connection: %v", err)
			return nil, err
		}
		if err = profileRights(act, in); err != nil {
			c.Logger.Errorf("account.updateProfile - business connection: %v", err)
			return nil, err
		}
		c.MD.UserId = act.Owner()
	}

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
		if aboutTooLong(in.GetAbout().GetValue()) {
			err = mtproto.ErrAboutTooLong
			c.Logger.Errorf("account.updateProfile - error: %v", err)
			return nil, err
		}

		if in.GetAbout().GetValue() != me.About() {
			// biz answers false when the write failed.
			saved, err := c.svcCtx.Dao.UserClient.UserUpdateAbout(c.ctx, &userpb.TLUserUpdateAbout{
				UserId: c.MD.UserId,
				About:  in.GetAbout().GetValue(),
			})
			if err == nil && !mtproto.FromBool(saved) {
				err = mtproto.ErrInternalServerError
			}
			if err != nil {
				c.Logger.Errorf("account.updateProfile - error: %v", err)
				return nil, err
			}
			me.SetAbout(in.GetAbout().GetValue())
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

	if act != nil {
		return c.userForBot(act.BotID(), act.Owner())
	}
	return me.ToSelfUser(), nil
}

// profileRights checks that the bot of act may make the changes of in: the
// names with edit_name, the bio with edit_bio; a request that changes
// nothing is not the bot's either. BOT_ACCESS_FORBIDDEN otherwise.
func profileRights(act *business.Act, in *mtproto.TLAccountUpdateProfile) error {
	names := in.GetFirstName() != nil || in.GetLastName() != nil
	switch {
	case !names && in.GetAbout() == nil,
		names && !act.Has(business.RightEditName),
		in.GetAbout() != nil && !act.Has(business.RightEditBio):
		return mtproto.ErrBotAccessForbidden
	}
	return nil
}

// userForBot is user as botId sees it.
func (c *UserChannelProfilesCore) userForBot(botId, userId int64) (*mtproto.User, error) {
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{userId, botId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{botId},
	})
	if err != nil {
		c.Logger.Errorf("account.updateProfile - error: %v", err)
		return nil, err
	}
	list := users.GetUserListByIdList(botId, userId)
	if len(list) != 1 {
		return nil, mtproto.ErrUserIdInvalid
	}
	return list[0], nil
}
