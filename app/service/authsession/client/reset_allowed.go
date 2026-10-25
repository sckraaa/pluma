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

package authsessionclient

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// FreshAuthorizationAge is how old, in seconds, a session must be before it
// may terminate others (auth.resetAuthorizations, account.resetAuthorization),
// as in Telegram: someone who has just signed in with a stolen code cannot
// throw the owner out (task 008).
const FreshAuthorizationAge = 24 * 60 * 60

// CheckResetAllowed returns FRESH_RESET_AUTHORISATION_FORBIDDEN when the
// authorization of userId on authKeyId, the session that asks, was created
// less than FreshAuthorizationAge before now.
func CheckResetAllowed(ctx context.Context, c AuthsessionClient, userId, authKeyId, now int64) error {
	auths, err := c.AuthsessionGetAuthorizations(ctx, &authsession.TLAuthsessionGetAuthorizations{
		UserId:           userId,
		ExcludeAuthKeyId: authKeyId,
	})
	if err != nil {
		return err
	}
	for _, a := range auths.GetAuthorizations() {
		if a.GetCurrent() && now-int64(a.GetDateCreated()) < FreshAuthorizationAge {
			return mtproto.ErrFreshResetAuthorisationForbidden
		}
	}
	return nil
}
