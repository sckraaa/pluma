// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	bots_helper "github.com/teamgram/teamgram-server/app/bff/bots"
	groupcalls_helper "github.com/teamgram/teamgram-server/app/bff/groupcalls"
	statistics_helper "github.com/teamgram/teamgram-server/app/bff/statistics"
	voipcalls_helper "github.com/teamgram/teamgram-server/app/bff/voipcalls"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/mail"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/tgbot"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/teamgram/teamgram-server/pkg/webappdata"
	"github.com/teamgram/teamgram-server/pkg/webfile"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	KV                      kv.KvConf
	Mysql                   sqlx.Config // phone allowlist (pkg/phoneallowlist), 2FA passwords (pkg/twofa), push devices (pkg/pushdevices), mq_outbox
	PushSecretEncryptionKey string      // seals the secrets of push devices (task 009)
	LoginCode               logincode.Config
	BizServiceClient        zrpc.RpcClientConf
	AuthSessionClient       zrpc.RpcClientConf
	MediaClient             zrpc.RpcClientConf
	IdgenClient             zrpc.RpcClientConf
	MsgClient               zrpc.RpcClientConf
	SyncClient              SyncClientConf
	DfsClient               zrpc.RpcClientConf
	StatusClient            zrpc.RpcClientConf
	// Calls is the media side of one-to-one calls (docs/ops/calls.md); calls
	// are off without ICE servers.
	Calls voipcalls_helper.CallsConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// GroupCalls is the media side of group calls: the SFU nodes and the
	// client certificate of their control API (task 093,
	// docs/ops/group-calls.md); group calls are off without nodes.
	GroupCalls groupcalls_helper.GroupCallsConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ReleasesURL is the site the releases of the client apps are on,
	// https://pluma.chat (task 054): /dl/ of it serves them. Empty: no
	// update is offered and the desktop app keeps its own update prefix.
	ReleasesURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// IOSUpdate lets an Ad Hoc build of the iPhone app update from the app
	// itself (task 085): the secret of its tickets and the status address
	// of the install service. Without the secret such a build is offered
	// the site's install page.
	IOSUpdate iosupdate.Settings `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// The bot that fetches sticker sets from Telegram on demand (task 030);
	// none if empty.
	TelegramBot tgbot.Settings `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	// Transcribe is the speech engine of voice transcription and its limits
	// (task 090, docs/ops/transcription.md); transcription is off without
	// a URL.
	Transcribe transcribe.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Translate is the translation engine of messages and its limits (task
	// 101, ADR 0064, docs/ops/translation.md); the translation is off
	// without a URL.
	Translate translate.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Mail sends the codes of the recovery email of the cloud password
	// (task 091, ADR 0058, docs/ops/email.md); the recovery email is off
	// without a key.
	Mail mail.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// WebFile is upload.getWebFile (ADR 0062, p. 5, docs/ops/inline-bots.md):
	// the key that signs the addresses of inline bots' webDocument, shared
	// by bots (signing) and files (checking), and the cache of the files
	// fetched. No key: the addresses are not proxied.
	WebFile webfile.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Maps renders the map previews of geo points that clients ask for
	// with upload.getWebFile (task 100, ADR 0063, docs/ops/maps.md): the
	// maps service at home and the limits; maps are off without a URL.
	Maps staticmap.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// InlineBots is the built-in inline bots @gif and @pic (task 099, ADR
	// 0062, docs/ops/inline-bots.md): the providers that answer their
	// queries and the daily budget of the media they import into dfs.
	InlineBots bots_helper.InlineBotsConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// WebApp is the mini apps of bots (task 106, ADR 0068,
	// docs/ops/bots.md): the tgWebAppVersion by platform and the Ed25519
	// key of the signature of initData, WEBAPP_SIGNING_KEY.
	WebApp webappdata.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// BotSecrets seals the keys of bots derived from their tokens
	// (pkg/botkeys), BOT_SECRET_ENCRYPTION_KEY; shared by bots and
	// authorization (auth.importBotAuthorization stores them). Without it
	// the mini apps and the login of bots are off.
	BotSecrets botkeys.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Stats is the statistics of channels and groups (task 103, ADR 0066):
	// stats_dc and the key of the zoom tokens of the graphs; the statistics
	// are off without the key.
	Stats statistics_helper.StatsConfig
	// Avatars is the address of the public photos of users by username,
	// https://pluma.chat/_l/avatar/ (ADR 0036, p. 4), for the photo_url
	// bots are given.
	Avatars string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// SyncClientConf is where bff's pushes go: rows of mq_outbox for Topic in
// the database of Mysql, published by the relay of msg (ADR 0012, p. 4).
type SyncClientConf struct {
	Topic string
}
