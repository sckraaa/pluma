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

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	Name string
	// ListenOn is the HTTP address; Caddy sends the site's paths that are
	// no files of the site here (teamgramd/deploy/web/Caddyfile).
	ListenOn string
	Log      logx.LogConf
	// SiteURL is the link host's site, https://pluma.chat; WebURL the web
	// client, https://web.pluma.chat.
	SiteURL string
	WebURL  string
	// BizServiceClient reads users and channels, DfsClient their photos.
	BizServiceClient zrpc.RpcClientConf
	DfsClient        zrpc.RpcClientConf
	// Mysql holds the sticker sets of the catalog and the business chat
	// links (pkg/business).
	Mysql sqlx.Config

	// OAuth is the login of users on sites, oauth.pluma.chat (task 106,
	// ADR 0068 p. 6). Its requests live in the Redis of KV, which bff
	// answers the apps from; AuthsessionClient gives the region of a
	// visitor's address; BotSecrets opens the login keys of bots for the
	// format of the login widget.
	OAuth             OAuthConf
	KV                kv.KvConf
	AuthsessionClient zrpc.RpcClientConf
	BotSecrets        botkeys.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// OAuthConf is the provider of oauth.pluma.chat.
type OAuthConf struct {
	// Issuer is the provider's address, https://oauth.pluma.chat: the iss
	// of its ID tokens, and its host the host the provider answers on.
	Issuer string
	// SigningKeyFile holds the RSA keys of the ID tokens in PEM
	// (OAUTH_SIGNING_KEY_FILE): the first signs, any further one is still
	// published in the JWKS while a replaced key's tokens may be checked.
	// An empty file turns the provider off.
	SigningKeyFile string
}
