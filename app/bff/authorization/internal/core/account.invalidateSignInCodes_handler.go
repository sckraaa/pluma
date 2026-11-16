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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/logincode"
)

// AccountInvalidateSignInCodes
// account.invalidateSignInCodes#ca8ae8ba codes:Vector<string> = Bool;
//
// The codes the person has shown to someone: iOS sends those of the chat
// with the service notifications account when the person takes a
// screenshot of it (TelegramUI/Sources/ChatController.swift:7945-7958),
// core.telegram.org/api/auth "Invalidating login codes". Every pending
// login request of the account's phone with one of them is dropped
// (logincode.Invalidator). The answer is boolTrue whether or not a code
// matched: iOS does not read it (TelegramCore/Sources/Authorization.swift:
// 1428-1434), and any other answer would tell which codes are live.
func (c *AuthorizationCore) AccountInvalidateSignInCodes(in *mtproto.TLAccountInvalidateSignInCodes) (*mtproto.Bool, error) {
	// 400	BOT_METHOD_INVALID	This method can't be used by a bot
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.invalidateSignInCodes - error: %v", err)
		return nil, err
	}
	if len(in.GetCodes()) > logincode.MaxInvalidateCodes {
		err := mtproto.ErrLimitInvalid
		c.Logger.Errorf("account.invalidateSignInCodes - %d codes: %v", len(in.GetCodes()), err)
		return nil, err
	}

	user, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("account.invalidateSignInCodes - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	n, err := c.svcCtx.AuthLogic.Codes.Invalidate(c.ctx, user.Phone(), in.GetCodes())
	if err != nil {
		c.Logger.Errorf("account.invalidateSignInCodes - user %d: login code store: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if n > 0 {
		c.Logger.Infof("account.invalidateSignInCodes - user %d: %d login request(s) invalidated", c.MD.UserId, n)
	}
	return mtproto.BoolTrue, nil
}
