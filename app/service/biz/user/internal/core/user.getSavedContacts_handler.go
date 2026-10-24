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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserGetSavedContacts
// user.getSavedContacts user_id:long = Vector<SavedContact>;
//
// The phone book of the user on the server (contacts.getSaved): the
// contacts imported or added with a phone number and the imported numbers
// without an account the user may find.
func (c *UserCore) UserGetSavedContacts(in *user.TLUserGetSavedContacts) (*user.Vector_SavedContact, error) {
	list, err := c.svcCtx.Dao.SavedContacts(c.ctx, in.UserId)
	if err != nil {
		c.Logger.Errorf("user.getSavedContacts - user %d: %v", in.UserId, err)
		return nil, err
	}
	return &user.Vector_SavedContact{Datas: list}, nil
}
