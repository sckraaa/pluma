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
	"github.com/teamgram/teamgram-server/pkg/botgames"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/botwidgets"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/catalog/attached"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/sendas"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"github.com/teamgram/teamgram-server/pkg/webpage"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// StickerLists is where a sticker the account sends becomes a recent one
// (stickerlists.Store, task 079).
type StickerLists interface {
	Add(ctx context.Context, userID int64, list stickerlists.List, documentID int64) error
}

// LoginDomains gives the login domain of a bot, which the link of its login
// button must have (*botstore.Store, task 106).
type LoginDomains interface {
	LoginDomain(ctx context.Context, botId int64) (string, error)
}

// LoginCodes drops the pending login requests of a phone whose code the
// account has sent to a chat (logincode.Invalidator, task 098).
type LoginCodes interface {
	Invalidate(ctx context.Context, phone string, codes []string) (int, error)
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

	// Stories answers for the stories a message shares or replies to
	// (task 084); nil when the stories are not served.
	Stories plugin.StoryPlugin

	// LoginCodes invalidates the login codes the account sends or forwards
	// to a chat (task 098).
	LoginCodes LoginCodes

	// LoginDomains checks the links of the login buttons of bots (task
	// 106).
	LoginDomains LoginDomains

	// Games finds the games of bots that inputMediaGame names (task 106).
	Games botgames.Store

	// Widgets keeps the shared widgets of bots that inputMediaWidget makes
	// and inputMediaWidgetId names (task 106).
	Widgets botwidgets.Store

	// Business checks what a connected bot of Telegram Business does for
	// its account through its connection (task 105).
	Business *business.Store
}

func NewServiceContext(c config.Config, plugin plugin.MessagesPlugin, stories plugin.StoryPlugin) *ServiceContext {
	d := dao.New(c)
	db := sqlx.NewMySQL(&c.Mysql)
	if err := outbox.Register(outbox.Topics{Push: []string{c.SyncClient.Topic}}); err != nil {
		logx.Must(err)
	}
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		logx.Must(err)
	}
	// Only the HMAC key: the secrets of the review account and of the code
	// sender stay with authorization.
	loginCodes, err := logincode.NewInvalidator(logincode.Config{HmacKey: c.LoginCode.HmacKey}, kv.NewStore(c.KV))
	logx.Must(err)
	bots := botstore.New(db)
	return &ServiceContext{
		Config:       c,
		Dao:          d,
		Plugin:       plugin,
		Stories:      stories,
		Reactions:    db,
		Search:       db,
		Sublists:     db,
		SendAs:       sendas.New(db),
		StickerLists: stickerlists.NewStore(db, boxDB, c.SyncClient.Topic),
		Attached:     attached.NewStore(db),
		LoginCodes:   loginCodes,
		LoginDomains: bots,
		Games:        bots,
		Widgets:      bots,
		Business:     business.NewStore(db),
		WebPages: webpage.NewService(db, webpage.RPCPhotos{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: d.MediaClient,
		}),
	}
}
