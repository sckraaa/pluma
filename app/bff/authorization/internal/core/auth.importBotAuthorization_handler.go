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
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// AuthImportBotAuthorization
// auth.importBotAuthorization#67a3ff2c flags:int api_id:int api_hash:string bot_auth_token:string = auth.Authorization;
//
// The token names the bot and proves the right to it (pkg/botstore); a token
// no bot has, or one replaced, is ACCESS_TOKEN_INVALID; a deleted bot's token
// ACCESS_TOKEN_EXPIRED (core.telegram.org/method/auth.importBotAuthorization).
// No other login of the account is told: it has no human sessions to read it.
//
// The login also stores the keys derived from the token, which only a login
// and the issue of a token see in full (task 106, ADR 0068, p. 3).
func (c *AuthorizationCore) AuthImportBotAuthorization(in *mtproto.TLAuthImportBotAuthorization) (*mtproto.Auth_Authorization, error) {
	if err := c.svcCtx.Dao.CheckApiIdAndHash(in.ApiId, in.ApiHash); err != nil {
		c.Logger.Errorf("auth.importBotAuthorization - invalid api: %d", in.ApiId)
		return nil, err
	}
	botId, err := c.svcCtx.Bots.BotIdOfToken(c.ctx, in.BotAuthToken)
	if err != nil {
		c.Logger.Errorf("auth.importBotAuthorization - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if botId == 0 {
		return nil, mtproto.ErrAccessTokenInvalid
	}
	bot, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: botId})
	if err != nil {
		c.Logger.Errorf("auth.importBotAuthorization - bot %d: %v", botId, err)
		return nil, err
	}
	if !bot.IsBot() || bot.Deleted() {
		return nil, mtproto.ErrAccessTokenExpired
	}
	if _, err = c.svcCtx.Dao.AuthsessionClient.AuthsessionBindAuthKeyUser(c.ctx, &authsession.TLAuthsessionBindAuthKeyUser{
		AuthKeyId: c.MD.PermAuthKeyId,
		UserId:    botId,
	}); err != nil {
		c.Logger.Errorf("auth.importBotAuthorization - bind the key to bot %d: %v", botId, err)
		return nil, err
	}
	c.storeBotKeys(botId, in.BotAuthToken)
	return mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
		User: bot.ToSelfUser(),
	}).To_Auth_Authorization(), nil
}

// storeBotKeys stores the keys of token for botId when the bot has none of
// this token (pkg/botkeys). It runs after the login is bound: the login
// matters more, so a failure here is logged and the login still succeeds;
// the bot's mini apps and login button stay off until a later login or a
// new token stores the keys.
func (c *AuthorizationCore) storeBotKeys(botId int64, token string) {
	if c.svcCtx.BotKeys == nil || !c.svcCtx.BotKeys.Enabled() {
		return
	}
	_, tokenHash, ok := botstore.ParseToken(token)
	if !ok {
		return
	}
	if _, err := c.svcCtx.BotKeys.Ensure(c.ctx, botId, token, tokenHash); err != nil {
		c.Logger.Errorf("auth.importBotAuthorization - store the keys of bot %d: %v", botId, err)
	}
}
