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

package svc

import (
	"context"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/messages/plugin"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/catalog/attached"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/sendas"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"github.com/teamgram/teamgram-server/pkg/webpage"
	"github.com/zeromicro/go-zero/core/logx"
)

// StickerLists is where a sticker the account sends becomes a recent one
// (stickerlists.Store, task 079).
type StickerLists interface {
	Add(ctx context.Context, userID int64, list stickerlists.List, documentID int64) error
}

type ServiceContext struct {
	Config config.Config
	*dao.Dao
	Plugin plugin.MessagesPlugin
	// WebPages builds the cards of links (task 023).
	WebPages *webpage.Service
	// Reactions reads the tags of Saved Messages (task 053).
	Reactions *sqlx.DB
	// Search reads the boxes of users for messages.search and keeps the
	// quota of channels.searchPosts (task 066).
	Search *sqlx.DB
	// SendAs decides as whom the account writes to a supergroup (task 079).
	SendAs *sendas.Store
	// StickerLists keeps the recent stickers (task 079).
	StickerLists StickerLists
	// Attached keeps the stickers put on uploaded photos and videos (task
	// 084); nil: none are kept.
	Attached *attached.Store

	// Sublists is the database of the drafts of the sublists of monoforums
	// (task 084, pkg/monoforum), which a send with clear_draft clears.
	Sublists *sqlx.DB
}

func NewServiceContext(c config.Config, plugin plugin.MessagesPlugin) *ServiceContext {
	d := dao.New(c)
	db := sqlx.NewMySQL(&c.Mysql)
	if err := outbox.Register(outbox.Topics{Push: []string{c.SyncClient.Topic}}); err != nil {
		logx.Must(err)
	}
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		logx.Must(err)
	}
	return &ServiceContext{
		Config:       c,
		Dao:          d,
		Plugin:       plugin,
		Reactions:    db,
		Search:       db,
		Sublists:     db,
		SendAs:       sendas.New(db),
		StickerLists: stickerlists.NewStore(db, boxDB, c.SyncClient.Topic),
		Attached:     attached.NewStore(db),
		WebPages: webpage.NewService(db, webpage.RPCPhotos{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: d.MediaClient,
		}),
	}
}
