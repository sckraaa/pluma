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

// MessagesDeclineUrlAuth
// messages.declineUrlAuth#35436bbc url:string = Bool;
//
// The user declined a login. A login request of oauth.pluma.chat turns
// declined, and its page sends the site access_denied; declining it again
// is boolTrue, declining one accepted already is boolFalse, and one that is
// gone is URL_EXPIRED. A login button or a link of url_auth_domains keeps
// no state on the server until it is accepted, so declining one is
// boolTrue. tdesktop declines a button with its address
// (boxes/url_auth_box.cpp:387-392) and a link (:508-512, 586-592), Android
// and iOS a link (ui/OAuthSheet.java:387-410, TelegramCore
// RequestMessageActionCallback.swift:337-340); none of them reads the
// answer.
func (c *BotsCore) MessagesDeclineUrlAuth(in *mtproto.TLMessagesDeclineUrlAuth) (*mtproto.Bool, error) {
	token, ok := urlauth.OAuthToken(in.GetUrl())
	if !ok {
		return mtproto.BoolTrue, nil
	}
	declined, err := c.svcCtx.OAuth.Decline(c.ctx, token)
	if err != nil {
		err = oauthError(err)
		c.logOAuthError("messages.declineUrlAuth", err)
		return nil, err
	}
	return mtproto.ToBool(declined), nil
}
