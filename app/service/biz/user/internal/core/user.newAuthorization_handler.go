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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// UserNewAuthorization
// user.newAuthorization user_id:long except_auth_key_id:long update:Update = Bool;
//
// Journals updateNewAuthorization with seq for every authorization of the
// user but the one on the permanent key except_auth_key_id, and pushes it to
// their devices, in one box transaction (task 019, ADR 0022): the review
// banner of a new login reaches the devices that are offline too, and the
// new session does not see its own login.
func (c *UserCore) UserNewAuthorization(in *user.TLUserNewAuthorization) (*mtproto.Bool, error) {
	u := in.GetUpdate()
	if u.GetPredicateName() != mtproto.Predicate_updateNewAuthorization || in.GetUserId() == 0 {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("user.newAuthorization - user %d, %s: %v", in.GetUserId(), u.GetPredicateName(), err)
		return nil, err
	}
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.GetUserId(), nil, func(b *userbox.Box) error {
		return c.svcCtx.Dao.EmitSeqUpdateExcept(b, u, in.GetExceptAuthKeyId())
	})
	if err != nil {
		c.Logger.Errorf("user.newAuthorization - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
