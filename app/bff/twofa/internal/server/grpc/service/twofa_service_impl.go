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
	"github.com/teamgram/teamgram-server/app/bff/twofa/internal/core"
)

// AccountGetPassword
// account.getPassword#548a30f5 = account.Password;
func (s *Service) AccountGetPassword(ctx context.Context, request *mtproto.TLAccountGetPassword) (*mtproto.Account_Password, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getPassword - metadata: {%s}", c.MD)
	return c.AccountGetPassword(request)
}

// AccountGetPasswordSettings
// account.getPasswordSettings#9cd4eaf9 password:InputCheckPasswordSRP = account.PasswordSettings;
func (s *Service) AccountGetPasswordSettings(ctx context.Context, request *mtproto.TLAccountGetPasswordSettings) (*mtproto.Account_PasswordSettings, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getPasswordSettings - metadata: {%s}", c.MD)
	return c.AccountGetPasswordSettings(request)
}

// AccountUpdatePasswordSettings
// account.updatePasswordSettings#a59b102f password:InputCheckPasswordSRP new_settings:account.PasswordInputSettings = Bool;
func (s *Service) AccountUpdatePasswordSettings(ctx context.Context, request *mtproto.TLAccountUpdatePasswordSettings) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updatePasswordSettings - metadata: {%s}", c.MD)
	return c.AccountUpdatePasswordSettings(request)
}

// AccountDeclinePasswordReset
// account.declinePasswordReset#4c9409f6 = Bool;
func (s *Service) AccountDeclinePasswordReset(ctx context.Context, request *mtproto.TLAccountDeclinePasswordReset) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.declinePasswordReset - metadata: {%s}", c.MD)
	return c.AccountDeclinePasswordReset(request)
}
