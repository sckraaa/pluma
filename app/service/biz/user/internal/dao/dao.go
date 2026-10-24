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
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/config"
	idgen_client "github.com/teamgram/teamgram-server/app/service/idgen/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Dao dao.
type Dao struct {
	*Mysql
	sqlc.CachedConn
	media_client.MediaClient

	// BoxDB is the pool of box transactions (pkg/userbox): the *sql.DB of
	// Mysql. A phone book import runs in the importer's box transaction
	// and a contact's sign-up in the new user's, which writes the messages
	// to the importers as events (ADR 0011, 0012).
	BoxDB *sql.DB
	// Ids issues the dialog_message_id of the sign-up messages.
	Ids IdGenerator
	// Topics of the events.
	Topics config.Topics
}

// IdGenerator issues snowflake ids (idgen).
type IdGenerator interface {
	NextIds(ctx context.Context, num int) ([]int64, error)
}

// New new a dao and return.
func New(c config.Config) *Dao {
	if err := registerTopics(c.Topics); err != nil {
		panic(err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(err)
	}
	ids := idgen_client.NewIDGenClient2(rpcx.GetCachedRpcClient(c.IdgenClient))
	return &Dao{
		Mysql:       newMysqlDao(db),
		CachedConn:  sqlc.NewConn(db, c.Cache),
		MediaClient: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		BoxDB:       boxDB,
		Ids:         &ids,
		Topics:      c.Topics,
	}
}

// registerTopics declares the topics of the service's events and their
// classes (outbox.Register).
func registerTopics(t config.Topics) error {
	if t.Inbox == "" || t.Sync == "" {
		return fmt.Errorf("user: Topics.Inbox and Topics.Sync are required")
	}
	return outbox.Register(outbox.Topics{Inbox: []string{t.Inbox}, Push: []string{t.Sync}})
}
