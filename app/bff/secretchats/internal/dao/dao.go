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

package dao

import (
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/config"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// Dao holds the clients and the store of the secret chats.
type Dao struct {
	user_client.UserClient
	dfs_client.DfsClient
	sync_client.SyncClient
	Store *secretchat.Store
}

// New connects the clients.
func New(c config.Config) *Dao {
	return &Dao{
		UserClient: user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		DfsClient:  dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
		SyncClient: sync_client.MustNewSyncOutboxClient(c.SyncClient),
		Store:      secretchat.MustOpen(c.Mysql.DSN),
	}
}
