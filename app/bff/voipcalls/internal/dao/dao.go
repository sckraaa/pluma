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
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/config"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	status_client "github.com/teamgram/teamgram-server/app/service/status/client"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"
)

// Dao holds the calls table and the clients the calls use.
type Dao struct {
	*phonecalls.Store
	user_client.UserClient
	msg_client.MsgClient
	status_client.StatusClient
	sync_client.SyncClient
	// Moderation answers whether the caller is restricted (task 025).
	Moderation *moderation.DAO
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	return &Dao{
		Store:        phonecalls.NewStore(db),
		Moderation:   moderation.New(db),
		UserClient:   user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		MsgClient:    msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		StatusClient: status_client.NewStatusClient(rpcx.GetCachedRpcClient(c.StatusClient)),
		SyncClient:   sync_client.MustNewSyncOutboxClient(c.SyncClient),
	}
}
