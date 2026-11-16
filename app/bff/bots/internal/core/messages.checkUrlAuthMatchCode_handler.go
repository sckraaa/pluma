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
	"github.com/teamgram/teamgram-server/pkg/urlauth"
)

// MessagesCheckUrlAuthMatchCode
// messages.checkUrlAuthMatchCode#c9a47b0b url:string match_code:string = Bool;
//
// The user picked a match code of a login request of oauth.pluma.chat
// before confirming it (urlAuthResultRequest.match_codes_first): boolTrue
// for the code the page shows, after which the app asks for the
// confirmation and sends the code again with acceptUrlAuth; boolFalse for
// another, which the apps take for a failed login (Android
// ui/OAuthSheet.java:557-575, tdesktop boxes/url_auth_box.cpp:911-926). A
// wrong code counts towards the limit of the request, after which it is
// gone (URL_EXPIRED). Only a link of a login request has match codes:
// anything else is URL_INVALID.
func (c *BotsCore) MessagesCheckUrlAuthMatchCode(in *mtproto.TLMessagesCheckUrlAuthMatchCode) (*mtproto.Bool, error) {
	token, ok := urlauth.OAuthToken(in.GetUrl())
	if !ok {
		return nil, mtproto.ErrUrlInvalid
	}
	res, err := c.svcCtx.OAuth.CheckCode(c.ctx, token, in.GetMatchCode())
	if err != nil {
		err = oauthError(err)
		c.logOAuthError("messages.checkUrlAuthMatchCode", err)
		return nil, err
	}
	return mtproto.ToBool(res == urlauth.CodeRight), nil
}
