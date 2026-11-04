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
	"context"
	"database/sql"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/config"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Dao reads channels and their participants (the tables of ADR 0034, which
// pkg/channelbox and the box transactions of the writers change). Nothing
// is cached: a channel's row and a participant's row are read by primary
// key, and a cache would need every writer to drop its keys after commit.
type Dao struct {
	*Mysql
	// Photos loads the photos of channels: biz media.
	Photos PhotoSource
	// BoxDB is the pool of box transactions (pkg/userbox): the *sql.DB of
	// the service's MySQL, with the lock wait timeout of its DSN.
	BoxDB  *sql.DB
	Topics config.Topics
}

type PhotoSource interface {
	MediaGetPhoto(ctx context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error)
}

func New(c config.Config) *Dao {
	if c.Topics.Sync == "" {
		panic("channel: Topics.Sync is required")
	}
	if err := outbox.Register(outbox.Topics{Push: []string{c.Topics.Sync}}); err != nil {
		panic(err)
	}
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(err)
	}
	d := NewDao(sqlx.NewMySQL(&c.Mysql), media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)))
	d.BoxDB = boxDB
	d.Topics = c.Topics
	return d
}

func NewDao(db *sqlx.DB, photos PhotoSource) *Dao {
	return &Dao{
		Mysql:  newMysqlDao(db),
		Photos: photos,
	}
}

// RunBox runs fn as a box transaction of userId (userbox.Run) and wakes the
// relays of the process when it wrote events.
func (d *Dao) RunBox(ctx context.Context, userId int64, fn func(b *userbox.Box) error) (*userbox.Result, error) {
	res, err := userbox.Run(ctx, d.BoxDB, userId, fn)
	if err != nil {
		return nil, err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return res, nil
}

// EmitSeqUpdate journals u with the next seq of b's account and pushes it to
// every device of the account (sync_client.EmitSeqUpdate, ADR 0020). users
// and chats are what u refers to, as the account sees them.
func (d *Dao) EmitSeqUpdate(b *userbox.Box, u *mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat) error {
	return sync_client.EmitSeqUpdate(b, d.Topics.Sync, u, users, chats)
}
