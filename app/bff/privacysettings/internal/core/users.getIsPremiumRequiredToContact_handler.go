// Copyright 2024 Teamgram Authors
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
)

// UsersGetIsPremiumRequiredToContact
// users.getIsPremiumRequiredToContact#a622aa10 id:Vector<InputUser> = Vector<Bool>;
//
// Whether writing to each user needs Telegram Premium, at the same offset
// as the user: never — Pluma has no Premium, nobody can require it.
func (c *PrivacySettingsCore) UsersGetIsPremiumRequiredToContact(in *mtproto.TLUsersGetIsPremiumRequiredToContact) (*mtproto.Vector_Bool, error) {
	if err := c.checkUsers(in.GetId()); err != nil {
		c.Logger.Errorf("users.getIsPremiumRequiredToContact - error: %v", err)
		return nil, err
	}

	result := &mtproto.Vector_Bool{Datas: make([]*mtproto.Bool, 0, len(in.GetId()))}
	for range in.GetId() {
		result.Datas = append(result.Datas, mtproto.BoolFalse)
	}
	return result, nil
}
