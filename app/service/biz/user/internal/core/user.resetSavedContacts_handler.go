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
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// UserResetSavedContacts
// user.resetSavedContacts user_id:long = Bool;
//
// contacts.resetSaved: "removes all contacts without an associated Telegram
// account" — the imported numbers kept in the phone book; the user's
// contacts stay. The clients call it when the user turns contact sync off.
// In the user's box transaction, so that it does not interleave with an
// import of the same user.
func (c *UserCore) UserResetSavedContacts(in *user.TLUserResetSavedContacts) (*mtproto.Bool, error) {
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		_, err := dao.ResetSavedPhones(b)
		return err
	})
	if err != nil {
		c.Logger.Errorf("user.resetSavedContacts - user %d: %v", in.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
