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
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/config"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/stories"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// Dao holds the tables of the stories and the clients the methods use.
type Dao struct {
	*stories.Store
	user_client.UserClient
	channel_client.ChannelClient
	media_client.MediaClient
	msg_client.MsgClient
	sync_client.SyncClient
	// DB reads what the stories need of other tables: who is in the chats
	// a story names, and where a user may post (pkg/stories).
	DB *sqlx.DB
	// Reports is the moderation queue stories.report files into (ADR 0040).
	Reports *moderation.DAO
	// BoxDB runs the box transactions of a user (pkg/userbox): a change of
	// the blocklist of stories with its updatePeerBlocked with seq
	// (ADR 0020), to SyncTopic.
	BoxDB     *sql.DB
	SyncTopic string
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		logx.Must(err)
	}
	return &Dao{
		BoxDB:         boxDB,
		SyncTopic:     c.SyncClient.Topic,
		Store:         stories.NewStore(db),
		DB:            db.Master(),
		Reports:       moderation.New(db),
		UserClient:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		ChannelClient: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		MediaClient:   media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		MsgClient:     msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		SyncClient:    sync_client.MustNewSyncOutboxClient(c.SyncClient),
	}
}
