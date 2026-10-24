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
	dialogclient "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/premium"
)

// UsersGetRequirementsToContact
// users.getRequirementsToContact#d89a83a3 id:Vector<InputUser> = Vector<RequirementToContact>;
//
// What writing to each user requires, at the same offset as the user:
// Premium (requirementToContactPremium) where the user lets only contacts,
// chats there already and Premium users write (pkg/premium), nothing
// otherwise; Pluma has no paid messages.
func (c *PrivacySettingsCore) UsersGetRequirementsToContact(in *mtproto.TLUsersGetRequirementsToContact) (*mtproto.Vector_RequirementToContact, error) {
	required, err := c.premiumRequired(in.GetId())
	if err != nil {
		c.Logger.Errorf("users.getRequirementsToContact - error: %v", err)
		return nil, err
	}
	result := &mtproto.Vector_RequirementToContact{Datas: make([]*mtproto.RequirementToContact, 0, len(required))}
	for _, r := range required {
		if r {
			result.Datas = append(result.Datas, mtproto.MakeTLRequirementToContactPremium(nil).To_RequirementToContact())
		} else {
			result.Datas = append(result.Datas, mtproto.MakeTLRequirementToContactEmpty(nil).To_RequirementToContact())
		}
	}
	return result, nil
}

// premiumRequired tells, at the offset of each user, whether the requester
// needs Premium to write to the user (premium.RequiredToContact). As the
// methods that take users do (users.getUsers), a user that is not there
// fails the request: the answer is by offset.
func (c *PrivacySettingsCore) premiumRequired(inputs []*mtproto.InputUser) ([]bool, error) {
	ids := make([]int64, 0, len(inputs)+1)
	for _, input := range inputs {
		peer := mtproto.FromInputUser(c.MD.UserId, input)
		if !peer.IsUser() {
			return nil, mtproto.ErrUserIdInvalid
		}
		ids = append(ids, peer.PeerId)
	}
	if len(ids) == 0 {
		return []bool{}, nil
	}
	// The requester is loaded too, and in the scope of the contacts of
	// every user asked about.
	scope := append(ids, c.MD.UserId)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: scope,
		To: scope,
	})
	if err != nil {
		return nil, err
	}
	if !users.CheckExistUser(ids...) {
		return nil, mtproto.ErrUserIdInvalid
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	if !ok {
		return nil, mtproto.ErrUserIdInvalid
	}
	required := make([]bool, 0, len(ids))
	for _, id := range ids {
		target, _ := users.GetImmutableUser(id)
		r, err := premium.RequiredToContact(me, target, func() (bool, error) {
			return dialogclient.HasPrivateChat(c.ctx, c.svcCtx.Dao.DialogClient, id, c.MD.UserId)
		})
		if err != nil {
			return nil, err
		}
		required = append(required, r)
	}
	return required, nil
}
