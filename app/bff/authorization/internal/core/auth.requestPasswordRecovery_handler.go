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
)

// AuthRequestPasswordRecovery
// auth.requestPasswordRecovery#d897bc66 = auth.PasswordRecovery;
//
// There is no mail on the server, so no recovery email (account.password
// has has_recovery = false): PASSWORD_RECOVERY_NA, as Telegram answers an
// account without one. Clients then offer the reset of the account
// (account.deleteAccount); a logged-in user resets the password with
// account.resetPassword, others ask the administrator
// (pluma-admin password reset).
func (c *AuthorizationCore) AuthRequestPasswordRecovery(in *mtproto.TLAuthRequestPasswordRecovery) (*mtproto.Auth_PasswordRecovery, error) {
	return nil, mtproto.ErrPasswordRecoveryNa
}
