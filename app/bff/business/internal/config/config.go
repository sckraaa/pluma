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
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	// Mysql keeps the settings (pkg/business) and the catalog of time
	// zones, and runs the box transactions of the account (pkg/userbox)
	// whose updates go out by the outbox of SyncClient.Topic.
	Mysql       sqlx.Config
	UserClient  zrpc.RpcClientConf
	MediaClient zrpc.RpcClientConf
	SyncClient  sync_client.OutboxConf
	// MsgClient keeps the quick replies (service msg): their messages are
	// made and sent as other messages are.
	MsgClient zrpc.RpcClientConf
}
