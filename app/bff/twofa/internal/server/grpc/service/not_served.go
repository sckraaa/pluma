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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
)

// The recovery email methods of mtproto.RPCTwoFa: there is no mail on the
// server, so no email can be set (account.updatePasswordSettings refuses
// one) and none waits for confirmation. They answer ERR_ENTERPRISE_IS_BLOCKED
// (400), as before the service was served here: a client gives up on a
// 400, while it retries an internal error.

// AccountConfirmPasswordEmail
// account.confirmPasswordEmail#8fdf1920 code:string = Bool;
func (s *Service) AccountConfirmPasswordEmail(ctx context.Context, request *mtproto.TLAccountConfirmPasswordEmail) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountResendPasswordEmail
// account.resendPasswordEmail#7a7f2a15 = Bool;
func (s *Service) AccountResendPasswordEmail(ctx context.Context, request *mtproto.TLAccountResendPasswordEmail) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountCancelPasswordEmail
// account.cancelPasswordEmail#c1cbd5b6 = Bool;
func (s *Service) AccountCancelPasswordEmail(ctx context.Context, request *mtproto.TLAccountCancelPasswordEmail) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
