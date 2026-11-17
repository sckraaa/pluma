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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/core"
)

// RPCSeamless (task 106, ADR 0068 p. 6): the logins on bots' sites by a
// login button, a link of url_auth_domains or a login request of
// oauth.pluma.chat, and the sites logged in on.

// MessagesRequestUrlAuth
// messages.requestUrlAuth#894cc99c flags:# peer:flags.1?InputPeer msg_id:flags.1?int button_id:flags.1?int url:flags.2?string in_app_origin:flags.3?string = UrlAuthResult;
func (s *Service) MessagesRequestUrlAuth(ctx context.Context, request *mtproto.TLMessagesRequestUrlAuth) (*mtproto.UrlAuthResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.requestUrlAuth - metadata: {%s}", c.MD)
	return c.MessagesRequestUrlAuth(request)
}

// MessagesAcceptUrlAuth
// messages.acceptUrlAuth#18dd4e92 flags:# write_allowed:flags.0?true share_phone_number:flags.3?true peer:flags.1?InputPeer msg_id:flags.1?int button_id:flags.1?int url:flags.2?string match_code:flags.4?string tonconnect_session:flags.5?InputTonConnectOauthSession = UrlAuthResult;
func (s *Service) MessagesAcceptUrlAuth(ctx context.Context, request *mtproto.TLMessagesAcceptUrlAuth) (*mtproto.UrlAuthResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.acceptUrlAuth - metadata: {%s}", c.MD)
	return c.MessagesAcceptUrlAuth(request)
}

// AccountGetWebAuthorizations
// account.getWebAuthorizations#182e6d6f = account.WebAuthorizations;
func (s *Service) AccountGetWebAuthorizations(ctx context.Context, request *mtproto.TLAccountGetWebAuthorizations) (*mtproto.Account_WebAuthorizations, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getWebAuthorizations - metadata: {%s}", c.MD)
	return c.AccountGetWebAuthorizations(request)
}

// AccountResetWebAuthorization
// account.resetWebAuthorization#2d01b9ef hash:long = Bool;
func (s *Service) AccountResetWebAuthorization(ctx context.Context, request *mtproto.TLAccountResetWebAuthorization) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.resetWebAuthorization - metadata: {%s}", c.MD)
	return c.AccountResetWebAuthorization(request)
}

// AccountResetWebAuthorizations
// account.resetWebAuthorizations#682d2594 = Bool;
func (s *Service) AccountResetWebAuthorizations(ctx context.Context, request *mtproto.TLAccountResetWebAuthorizations) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.resetWebAuthorizations - metadata: {%s}", c.MD)
	return c.AccountResetWebAuthorizations(request)
}

// MessagesDeclineUrlAuth
// messages.declineUrlAuth#35436bbc url:string = Bool;
func (s *Service) MessagesDeclineUrlAuth(ctx context.Context, request *mtproto.TLMessagesDeclineUrlAuth) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.declineUrlAuth - metadata: {%s}", c.MD)
	return c.MessagesDeclineUrlAuth(request)
}

// MessagesCheckUrlAuthMatchCode
// messages.checkUrlAuthMatchCode#c9a47b0b url:string match_code:string = Bool;
func (s *Service) MessagesCheckUrlAuthMatchCode(ctx context.Context, request *mtproto.TLMessagesCheckUrlAuthMatchCode) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.checkUrlAuthMatchCode - metadata: {%s}", c.MD)
	return c.MessagesCheckUrlAuthMatchCode(request)
}
