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
	"database/sql"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/business/internal/config"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

type Dao struct {
	user_client.UserClient
	media_client.MediaClient
	// Store keeps the Business settings (pkg/business).
	Store *business.Store
	// Catalog gives the time zones of help.getTimezonesList (ADR 0028).
	Catalog *catalog.Store
	// BoxDB runs the box transactions of the account (pkg/userbox): a
	// change of its profile with updateUser with seq to its other devices
	// (ADR 0020), to SyncTopic.
	BoxDB     *sql.DB
	SyncTopic string
	// MsgClient serves the quick replies (task 105).
	MsgClient msg_client.MsgClient
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		logx.Must(err)
	}
	return &Dao{
		UserClient:  user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		MediaClient: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		Store:       business.NewStore(db),
		Catalog:     catalog.NewStore(db),
		BoxDB:       boxDB,
		SyncTopic:   c.SyncClient.Topic,
		MsgClient:   msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
	}
}
