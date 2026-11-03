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

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/config"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// Dao reads channels and their participants (the tables of ADR 0034, which
// pkg/channelbox and the box transactions of the writers change). Nothing
// is cached: a channel's row and a participant's row are read by primary
// key, and a cache would need every writer to drop its keys after commit.
type Dao struct {
	*Mysql
	// Photos loads the photos of channels: biz media.
	Photos PhotoSource
}

// PhotoSource loads a photo by its id: biz media.
type PhotoSource interface {
	MediaGetPhoto(ctx context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error)
}

func New(c config.Config) *Dao {
	return NewDao(sqlx.NewMySQL(&c.Mysql), media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)))
}

// NewDao is the Dao on db and photos.
func NewDao(db *sqlx.DB, photos PhotoSource) *Dao {
	return &Dao{
		Mysql:  newMysqlDao(db),
		Photos: photos,
	}
}
