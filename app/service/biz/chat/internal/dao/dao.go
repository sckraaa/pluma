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

package dao

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/plugin"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

type Dao struct {
	*Mysql
	sqlc.CachedConn
	media_client.MediaClient
	Plugin plugin.ChatPlugin

	// BoxDB is the pool of box transactions (pkg/userbox): the *sql.DB of
	// Mysql. A change of a basic group runs in the box transaction of the
	// user who makes it, with the chat's row locked, and writes the updates
	// with seq of the members as the transaction's events (ADR 0011, 0021).
	BoxDB *sql.DB
	// Users looks up the users of the operator's own updates with seq, as
	// the operator sees them: biz user.
	Users UserSource
	// Topics of the events.
	Topics config.Topics
}

// UserSource looks users up as one of them sees them: biz user.
type UserSource interface {
	UserGetMutableUsers(ctx context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error)
}

func New(c config.Config, plugin plugin.ChatPlugin, users UserSource) (dao *Dao) {
	if err := registerTopics(c.Topics); err != nil {
		panic(err)
	}
	if users == nil {
		panic("chat: no user source")
	}
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(err)
	}
	return &Dao{
		Mysql:       newMysqlDao(db),
		CachedConn:  sqlc.NewConn(db, c.Cache),
		MediaClient: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		Plugin:      plugin,
		BoxDB:       boxDB,
		Users:       users,
		Topics:      c.Topics,
	}
}

func registerTopics(t config.Topics) error {
	if t.Inbox == "" || t.Sync == "" {
		return fmt.Errorf("chat: Topics.Inbox and Topics.Sync are required")
	}
	return outbox.Register(outbox.Topics{Inbox: []string{t.Inbox}, Push: []string{t.Sync}})
}
