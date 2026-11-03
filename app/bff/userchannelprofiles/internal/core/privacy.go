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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// canSee answers whether the privacy rules of key of the user peerId let
// the caller see what they guard (task 034, ADR 0031). The account always
// sees its own; an unknown user is USER_ID_INVALID.
func (c *UserChannelProfilesCore) canSee(peerId int64, key int) (bool, error) {
	if peerId == c.MD.UserId {
		return true, nil
	}
	ids := []int64{c.MD.UserId, peerId}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      ids,
		Privacy: true,
		HasTo:   true,
		To:      ids,
	})
	if err != nil {
		return false, err
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	peer, ok2 := users.GetImmutableUser(peerId)
	if !ok || !ok2 {
		return false, mtproto.ErrUserIdInvalid
	}
	return peer.CheckPrivacyFor(key, me), nil
}
