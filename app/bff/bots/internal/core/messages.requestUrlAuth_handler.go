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

// MessagesRequestUrlAuth
// messages.requestUrlAuth#894cc99c flags:# peer:flags.1?InputPeer msg_id:flags.1?int button_id:flags.1?int url:flags.2?string in_app_origin:flags.3?string = UrlAuthResult;
//
// A login button pressed or a link to a domain of url_auth_domains opened
// (core.telegram.org/api/url-authorization). A user who logged in on the
// bot's domain before and is not asked for anything new is logged in at
// once: urlAuthResultAccepted with the address and the signed data, which
// tdesktop opens (boxes/url_auth_box.cpp:302-305). Otherwise
// urlAuthResultRequest asks the user, and messages.acceptUrlAuth logs in.
// A login the server takes no part in is urlAuthResultDefault: the client
// opens the link as it is.
func (c *BotsCore) MessagesRequestUrlAuth(in *mtproto.TLMessagesRequestUrlAuth) (*mtproto.UrlAuthResult, error) {
	t, err := c.urlAuthTarget(in.GetPeer(), int32Of(in.GetMsgId()), int32Of(in.GetButtonId()), stringOf(in.GetUrl()), stringOf(in.GetInAppOrigin()))
	if err != nil {
		return nil, err
	}
	if t == nil {
		return urlAuthDefault(), nil
	}
	allowed, err := c.writeAllowed(t)
	if err != nil {
		c.Logger.Errorf("messages.requestUrlAuth - error: %v", err)
		return nil, err
	}
	askWrite := t.askWrite && !allowed

	// Logged in before and asked nothing new: in at once. Whether Telegram
	// itself does so is not verified; the documentation lets the server
	// answer urlAuthResultAccepted to requestUrlAuth.
	session, err := c.svcCtx.Bots.WebAuthorizationOf(c.ctx, c.MD.UserId, t.bot.Id(), t.domain)
	if err != nil {
		c.Logger.Errorf("messages.requestUrlAuth - error: %v", err)
		return nil, err
	}
	if session != nil && !askWrite {
		link, err := c.signedLink(t)
		if err != nil {
			c.Logger.Errorf("messages.requestUrlAuth - error: %v", err)
			return nil, err
		}
		if err = c.svcCtx.Bots.TouchWebAuthorization(c.ctx, c.MD.UserId, session.Hash, nowUnix()); err != nil {
			c.Logger.Errorf("messages.requestUrlAuth - error: %v", err)
			return nil, err
		}
		return accepted(link), nil
	}

	users, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId, t.bot.Id()}})
	if err != nil {
		c.Logger.Errorf("messages.requestUrlAuth - error: %v", err)
		return nil, err
	}
	bot := users.GetUserListByIdList(c.MD.UserId, t.bot.Id())
	if len(bot) != 1 {
		return nil, mtproto.ErrBotInvalid
	}
	return mtproto.MakeTLUrlAuthResultRequest(&mtproto.UrlAuthResult{
		RequestWriteAccess: askWrite,
		Bot:                bot[0],
		Domain:             t.domain,
	}).To_UrlAuthResult(), nil
}
