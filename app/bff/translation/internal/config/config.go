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
	"github.com/teamgram/teamgram-server/pkg/translate"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	// Mysql holds translation_cache and peer_translation_settings
	// (migrate-20261119-translation.sql).
	Mysql sqlx.Config
	// KV keeps the per-user limits of the items sent to the engine.
	KV kv.KvConf
	// Translate is the engine and the limits (docs/ops/translation.md);
	// without a URL the translation is off.
	Translate translate.Config
	// MessageClient, ChannelClient and DialogClient read the messages
	// asked about, as their reader sees them; UserClient tells which
	// users a mention of a client's text names.
	MessageClient zrpc.RpcClientConf
	ChannelClient zrpc.RpcClientConf
	DialogClient  zrpc.RpcClientConf
	UserClient    zrpc.RpcClientConf
}
