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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/inline"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/webappdata"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config is what the methods of bots need: the tables of bots
// (pkg/botstore), Redis for the queries a bot answers, the box of a bot for
// its updates, and the services a press or a send goes through.
type Config struct {
	zrpc.RpcServerConf
	Mysql         sqlx.Config
	KV            kv.KvConf
	SyncClient    sync_client.OutboxConf
	UserClient    zrpc.RpcClientConf
	MessageClient zrpc.RpcClientConf
	ChatClient    zrpc.RpcClientConf
	ChannelClient zrpc.RpcClientConf
	DialogClient  zrpc.RpcClientConf
	MediaClient   zrpc.RpcClientConf
	MsgClient     zrpc.RpcClientConf
	// AuthsessionClient gives the app and the device of the session a user
	// logs in on a site from, and the region of its address (task 106).
	AuthsessionClient zrpc.RpcClientConf
	// DfsClient stores the media the built-in inline bots import
	// (pkg/inlinemedia).
	DfsClient zrpc.RpcClientConf
	// InlineBots is the providers of the built-in inline bots and the
	// budget of their imports (task 099, ADR 0062).
	InlineBots inline.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// WebFileKey signs the addresses of the thumbnails and content of
	// inline results, WEBFILE_HMAC_KEY (pkg/webfile, ADR 0062, p. 5); the
	// files service fetches only what it signs. Empty: webDocumentNoProxy.
	WebFileKey string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// WebApp is the mini apps of bots (task 106, ADR 0068): the
	// tgWebAppVersion by platform and WEBAPP_SIGNING_KEY, the Ed25519 key of
	// the signature of initData.
	WebApp webappdata.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// BotSecrets is BOT_SECRET_ENCRYPTION_KEY, under which the keys of bots
	// are sealed (pkg/botkeys); without it the mini apps and the login of
	// bots are off.
	BotSecrets botkeys.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Avatars is the address the public photo of a user with a username is
	// under, the username appended: photo_url of initData and of a login
	// (ADR 0036, p. 4). Empty: no photo_url.
	Avatars string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
