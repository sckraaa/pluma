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
	groupcalls_helper "github.com/teamgram/teamgram-server/app/bff/groupcalls"
	voipcalls_helper "github.com/teamgram/teamgram-server/app/bff/voipcalls"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/mail"
	"github.com/teamgram/teamgram-server/pkg/tgbot"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
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
	// Mail sends the codes of the recovery email of the cloud password
	// (task 091, ADR 0058, docs/ops/email.md); the recovery email is off
	// without a key.
	Mail mail.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// SyncClientConf is where bff's pushes go: rows of mq_outbox for Topic in
// the database of Mysql, published by the relay of msg (ADR 0012, p. 4).
type SyncClientConf struct {
	Topic string
}
