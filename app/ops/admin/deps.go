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

package main

import (
	"context"
	"net/http"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicemsg"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"

	"github.com/zeromicro/go-zero/core/logx"
)

// The services a request of the panel reaches. Destructive actions always go
// through them: the panel writes only its own tables and the journal.
type channelRPC interface {
	ChannelSetBlocked(ctx context.Context, in *channel.TLChannelSetBlocked) (*mtproto.Bool, error)
	ChannelDeleteChannel(ctx context.Context, in *channel.TLChannelDeleteChannel) (*mtproto.Updates, error)
}

type chatRPC interface {
	ChatSetBlocked(ctx context.Context, in *chat.TLChatSetBlocked) (*mtproto.Bool, error)
	ChatDeleteChat(ctx context.Context, in *chat.TLChatDeleteChat) (*mtproto.MutableChat, error)
}

type userRPC interface {
	UserDeleteUser(ctx context.Context, in *user.TLUserDeleteUser) (*mtproto.Bool, error)
	UserDeleteUsername(ctx context.Context, in *user.TLUserDeleteUsername) (*mtproto.Bool, error)
}

type authRPC interface {
	AuthsessionGetAuthorizations(ctx context.Context, in *authsession.TLAuthsessionGetAuthorizations) (*mtproto.Account_Authorizations, error)
}

// codeIssuer gives a login code for a pending request of a phone, the path
// of tools/pluma-admin login-code.
type codeIssuer interface {
	Pending(ctx context.Context, phone string) ([]logincode.PendingRequest, error)
	Reissue(ctx context.Context, phone, requestId string) (*logincode.Reissued, error)
}

// passwordForgetter removes the 2FA password of a deleted account
// (pkg/twofa, the same step account.deleteAccount takes).
type passwordForgetter interface {
	Forget(ctx context.Context, userId int64) error
}

// outboxOps are the operator's actions on mq_outbox (pkg/outbox).
type outboxOps interface {
	list(ctx context.Context, states ...int8) ([]outboxRow, error)
	replay(ctx context.Context, recipient int64) (int, error)
	discard(ctx context.Context, id int64) (outboxRow, error)
}

// metricsAPI reads the state of the services from Prometheus.
type metricsAPI interface {
	services(ctx context.Context) ([]serviceState, error)
	alerts(ctx context.Context) ([]alertState, error)
	disk(ctx context.Context) (*diskState, error)
}

// releasesAPI is the releases service (app/service/releases).
type releasesAPI interface {
	list(ctx context.Context) ([]platformReleases, error)
	setWithdrawn(ctx context.Context, platform string, build int32, withdrawn bool) (*platformReleases, error)
}

// deps are the panel's dependencies.
type deps struct {
	dao       *moderation.DAO
	runner    *effects.Runner
	dfs       dfsClient
	channel   channelRPC
	chat      chatRPC
	user      userRPC
	auth      authRPC
	msg       servicemsg.Msg
	codes     codeIssuer
	twofa     passwordForgetter
	allowlist *phoneallowlist.DAO
	outbox    outboxOps
	prom      metricsAPI
	releases  releasesAPI
}

// fail answers an unexpected error: logged in full, nothing of it to the
// client.
func (s *server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	logx.WithContext(r.Context()).Errorf("admin: %s: %v", what, err)
	httpError(w, http.StatusInternalServerError, "internal error")
}

// journal writes the journal first, as every action of the panel does: an
// action the journal did not record does not happen. false means the
// response is already written.
func (s *server) journal(w http.ResponseWriter, r *http.Request, a *actor, action, target, detail string) bool {
	if err := s.dao.Audit(r.Context(), a.login, action, target, detail); err != nil {
		s.fail(w, r, "audit", err)
		return false
	}
	return true
}
