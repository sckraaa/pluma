// Copyright 2025 Teamgram Authors
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
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UsersGetRequirementsToContact
// users.getRequirementsToContact#d89a83a3 id:Vector<InputUser> = Vector<RequirementToContact>;
//
// What writing to each user requires, at the same offset as the user:
// nothing — Pluma has neither Premium nor paid messages, the requirements
// a user may set in Telegram.
func (c *PrivacySettingsCore) UsersGetRequirementsToContact(in *mtproto.TLUsersGetRequirementsToContact) (*mtproto.Vector_RequirementToContact, error) {
	if err := c.checkUsers(in.GetId()); err != nil {
		c.Logger.Errorf("users.getRequirementsToContact - error: %v", err)
		return nil, err
	}

	result := &mtproto.Vector_RequirementToContact{Datas: make([]*mtproto.RequirementToContact, 0, len(in.GetId()))}
	for range in.GetId() {
		result.Datas = append(result.Datas, mtproto.MakeTLRequirementToContactEmpty(nil).To_RequirementToContact())
	}
	return result, nil
}

// checkUsers checks that the users of a bulk request exist, as the
// methods that take users do (users.getUsers): the answer is by offset, so
// a user that is not there fails the request.
func (c *PrivacySettingsCore) checkUsers(inputs []*mtproto.InputUser) error {
	ids := make([]int64, 0, len(inputs))
	for _, input := range inputs {
		peer := mtproto.FromInputUser(c.MD.UserId, input)
		if !peer.IsUser() {
			return mtproto.ErrUserIdInvalid
		}
		ids = append(ids, peer.PeerId)
	}
	if len(ids) == 0 {
		return nil
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: ids,
	})
	if err != nil {
		return err
	}
	if !users.CheckExistUser(ids...) {
		return mtproto.ErrUserIdInvalid
	}
	return nil
}
