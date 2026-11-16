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
	"context"
	"time"

	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/contextx"
)

// loginCodesTimeout bounds the invalidation that follows a send: the send
// is done and answered whatever happens to it.
const loginCodesTimeout = 2 * time.Second

// invalidateSentLoginCodes drops the pending login requests of the
// account's phone whose code is in texts, the texts it has just sent,
// forwarded or edited into a chat: "Telegram servers automatically
// invalidate login codes that are sent or forwarded to other chats"
// (core.telegram.org/api/auth, "Invalidating login codes"). Every chat
// counts, Saved Messages too: a code is meant for no one, and a new one is
// one request away. A connected business bot that writes into the
// account's chats (businessAct) puts the account's codes there just the
// same, so only a bot's own messages are skipped.
//
// It runs after msg has accepted the message, in the request's goroutine,
// for at most loginCodesTimeout. Only a text with a run of exactly
// logincode.Length digits costs anything (one user lookup and one read of
// Redis); a failure is logged without the codes, and the send stands.
func (c *MessagesCore) invalidateSentLoginCodes(texts ...string) {
	if (c.MD.IsBot && c.business == nil) || c.svcCtx.LoginCodes == nil {
		return
	}
	var codes []string
	for _, text := range texts {
		codes = append(codes, logincode.Candidates(text)...)
	}
	if len(codes) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(contextx.ValueOnlyFrom(c.ctx), loginCodesTimeout)
	defer cancel()
	user, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("invalidate sent login codes - user %d: %v", c.MD.UserId, err)
		return
	}
	n, err := c.svcCtx.LoginCodes.Invalidate(ctx, user.Phone(), codes)
	if err != nil {
		c.Logger.Errorf("invalidate sent login codes - user %d: login code store: %v", c.MD.UserId, err)
	}
	if n > 0 {
		c.Logger.Infof("invalidate sent login codes - user %d: %d login request(s) invalidated", c.MD.UserId, n)
	}
}
