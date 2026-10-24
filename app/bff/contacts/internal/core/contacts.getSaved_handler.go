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

	"google.golang.org/grpc/status"
)

// errTakeoutRequired: contacts.getSaved is a method of the takeout API, it
// is called wrapped in invokeWithTakeout (403, as Telegram answers).
var errTakeoutRequired = status.Error(mtproto.ErrForbidden, "TAKEOUT_REQUIRED")

// ContactsGetSaved
// contacts.getSaved#82f1e39f = Vector<SavedContact>;
//
// "Get all contacts, requires a takeout session": the phone book the user
// synced, the numbers that are contacts and those that are not
// (user.getSavedContacts). Telegram Desktop calls it in a data export.
func (c *ContactsCore) ContactsGetSaved(in *mtproto.TLContactsGetSaved) (*mtproto.Vector_SavedContact, error) {
	if c.MD.GetTakeout().GetId() == 0 {
		c.Logger.Errorf("contacts.getSaved - error: %v", errTakeoutRequired)
		return nil, errTakeoutRequired
	}

	saved, err := c.svcCtx.Dao.UserClient.UserGetSavedContacts(c.ctx, &userpb.TLUserGetSavedContacts{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("contacts.getSaved - error: %v", err)
		return nil, err
	}

	return &mtproto.Vector_SavedContact{
		Datas: saved.GetDatas(),
	}, nil
}
