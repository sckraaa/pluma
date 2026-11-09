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
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"

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
}
