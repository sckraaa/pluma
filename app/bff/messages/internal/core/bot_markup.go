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
	"errors"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
)

// botMarkup is the markup a message of c's user carries: a bot's, checked
// and with its input buttons turned into received ones (task 070), and none
// from a user account: the keyboards are for bots
// (core.telegram.org/api/bots/buttons: "Bots can attach a ReplyMarkup").
// game tells that the message carries a game, the only message whose
// keyboard may have the game's button (botmarkup.Env.Game, task 106).
func (c *MessagesCore) botMarkup(markup *mtproto.ReplyMarkup, game bool) (*mtproto.ReplyMarkup, error) {
	if markup == nil {
		return nil, nil
	}
	// The bot of a business connection writes the keyboard of the
	// account's message (task 105).
	self := c.markupBot()
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: self})
	if err != nil {
		return nil, err
	}
	if !me.IsBot() {
		return nil, nil
	}
	return botmarkup.Normalize(markup, botmarkup.Env{
		Self: self,
		ResolveUser: func(in *mtproto.InputUser) int64 {
			u := mtproto.FromInputUser(self, in)
			if !u.IsUser() {
				return 0
			}
			if _, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: u.PeerId}); err != nil {
				return 0
			}
			return u.PeerId
		},
		LoginDomain: c.loginDomain,
		Game:        game,
	})
}

// loginDomain is the login domain of a user that is a bot (task 106);
// isBot is false for a user that is not one or no longer exists.
func (c *MessagesCore) loginDomain(userId int64) (string, bool, error) {
	u, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: userId})
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !u.IsBot() || u.Deleted() {
		return "", false, nil
	}
	if c.svcCtx.LoginDomains == nil {
		return "", true, nil
	}
	domain, err := c.svcCtx.LoginDomains.LoginDomain(c.ctx, userId)
	if err != nil {
		c.Logger.Errorf("bot markup - login domain of %d: %v", userId, err)
		return "", true, mtproto.ErrInternalServerError
	}
	return domain, true, nil
}
