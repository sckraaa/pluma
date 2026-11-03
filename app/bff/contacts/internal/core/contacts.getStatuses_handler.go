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
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactsGetStatuses
// contacts.getStatuses#c4a353ee = Vector<ContactStatus>;
func (c *ContactsCore) ContactsGetStatuses(in *mtproto.TLContactsGetStatuses) (*mtproto.Vector_ContactStatus, error) {
	cList, err := c.svcCtx.Dao.UserClient.UserGetContactList(c.ctx, &userpb.TLUserGetContactList{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("contacts.getStatuses - error: %v", err)
		return nil, err
	}

	rList := &mtproto.Vector_ContactStatus{
		Datas: make([]*mtproto.ContactStatus, 0, len(cList.GetDatas())),
	}
	if len(cList.GetDatas()) == 0 {
		return rList, nil
	}

	idList := make([]int64, 0, len(cList.GetDatas()))
	for _, id := range cList.GetDatas() {
		idList = append(idList, id.ContactUserId)
	}

	// The statuses go through the same privacy check as users.getUsers, in
	// both directions: each contact's rule for us and ours for the contact,
	// so both users' contact rows are loaded.
	all := append([]int64{c.MD.UserId}, idList...)
	mUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: all,
		To: all,
	})
	if err != nil {
		c.Logger.Errorf("contacts.getStatuses - error: %v", err)
		return nil, err
	}

	for _, u := range mUsers.GetUserListByIdList(c.MD.UserId, idList...) {
		if u.GetStatus() == nil {
			continue
		}
		rList.Datas = append(rList.Datas, mtproto.MakeTLContactStatus(&mtproto.ContactStatus{
			UserId: u.GetId(),
			Status: u.GetStatus(),
		}).To_ContactStatus())
	}

	return rList, nil
}
