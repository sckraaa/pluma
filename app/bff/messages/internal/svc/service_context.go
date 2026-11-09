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
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/messages/plugin"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/webpage"
)

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
}

func NewServiceContext(c config.Config, plugin plugin.MessagesPlugin) *ServiceContext {
	d := dao.New(c)
	db := sqlx.NewMySQL(&c.Mysql)
	return &ServiceContext{
		Config:    c,
		Dao:       d,
		Plugin:    plugin,
		Reactions: db,
		Search:    db,
		WebPages: webpage.NewService(db, webpage.RPCPhotos{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: d.MediaClient,
		}),
	}
}
