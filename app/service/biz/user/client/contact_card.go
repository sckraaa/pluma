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

package userclient

import (
	"context"

	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactCardUserId is the account a contact card with the normalized
// phone, sent by senderId, links to (messageMediaContact.user_id), or 0
// when the number has none. The support ghost's number links to no account
// but for the ghost itself (ADR 0048): the card would tell its sender and
// every recipient whose the number is.
func ContactCardUserId(ctx context.Context, cli UserClient, senderId int64, phone string) (int64, error) {
	id, err := cli.UserGetUserIdByPhone(ctx, &user.TLUserGetUserIdByPhone{Phone: phone})
	if err != nil || id.GetV() == 0 {
		// A number without an account, as it always was: the card keeps
		// the name and the number alone.
		return 0, nil //nolint:nilerr // no account behind the number is the common case
	}
	if id.GetV() == senderId {
		return id.GetV(), nil
	}
	owner, err := cli.UserGetImmutableUser(ctx, &user.TLUserGetImmutableUser{Id: id.GetV()})
	if err != nil {
		return 0, err
	}
	if owner.Ghost() {
		return 0, nil
	}
	return id.GetV(), nil
}
