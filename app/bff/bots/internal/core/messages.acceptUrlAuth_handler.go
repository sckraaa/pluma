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
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesAcceptUrlAuth
// messages.acceptUrlAuth#18dd4e92 flags:# write_allowed:flags.0?true share_phone_number:flags.3?true peer:flags.1?InputPeer msg_id:flags.1?int button_id:flags.1?int url:flags.2?string match_code:flags.4?string tonconnect_session:flags.5?InputTonConnectOauthSession = UrlAuthResult;
//
// The user agreed to log in: the login is recorded among the sites the user
// logged in on (account.getWebAuthorizations) and the answer is the
// address with the signed data, which the client opens (tdesktop
// boxes/url_auth_box.cpp:400-427). write_allowed lets the bot write to the
// user when the button asked for it ("if requested",
// core.telegram.org/method/messages.acceptUrlAuth); the first permission
// posts messageActionBotAllowed with the domain in the chat with the bot.
// A login button gives the site no phone number: share_phone_number belongs
// to the logins of sites through oauth.pluma.chat.
func (c *BotsCore) MessagesAcceptUrlAuth(in *mtproto.TLMessagesAcceptUrlAuth) (*mtproto.UrlAuthResult, error) {
	t, err := c.urlAuthTarget(in.GetPeer(), int32Of(in.GetMsgId()), int32Of(in.GetButtonId()), stringOf(in.GetUrl()), nil)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return urlAuthDefault(), nil
	}
	writeAllowed := in.GetWriteAllowed() && t.askWrite
	link, err := c.signedLink(t)
	if err != nil {
		c.Logger.Errorf("messages.acceptUrlAuth - error: %v", err)
		return nil, err
	}
	if err = c.recordWebLogin(t, writeAllowed); err != nil {
		c.Logger.Errorf("messages.acceptUrlAuth - error: %v", err)
		return nil, err
	}
	if writeAllowed {
		if _, err = c.allowWrite(t.bot.Id(), botstore.WriteAllowedDomain, mtproto.MakeMessageActionBotAllowed(t.domain)); err != nil {
			c.Logger.Errorf("messages.acceptUrlAuth - permission of bot %d: %v", t.bot.Id(), err)
			return nil, err
		}
	}
	return accepted(link), nil
}
