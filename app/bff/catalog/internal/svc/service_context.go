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

package svc

import (
	"context"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/config"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/demand"
	"github.com/teamgram/teamgram-server/pkg/catalog/importer"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/tgbot"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"github.com/zeromicro/go-zero/core/logx"
)

// Catalog is what the handlers read of the catalog (catalog.Store).
type Catalog interface {
	Answer(ctx context.Context, method, arg string) (*catalog.Answer, error)
	SetByID(ctx context.Context, id int64) (*catalog.Set, error)
	SetByShortName(ctx context.Context, name string) (*catalog.Set, error)
	SetByKind(ctx context.Context, kind string) (*catalog.Set, error)
	CatalogDocuments(ctx context.Context, ids []int64) ([]int64, error)
}

// Demand fetches a set from Telegram the catalog lacks (demand.Fetcher).
type Demand interface {
	ByShortName(ctx context.Context, name string) (*catalog.Set, error)
}

// Installed is the sets accounts installed (installed.Store).
type Installed interface {
	Installed(ctx context.Context, userID int64, kind installed.Kind) ([]installed.Entry, error)
	Archived(ctx context.Context, userID int64, kind installed.Kind) ([]installed.Entry, error)
	Entry(ctx context.Context, userID, setID int64) (*installed.Entry, error)
	Install(ctx context.Context, userID int64, kind installed.Kind, setID int64, archived bool, newSet func(installedAt int64) *mtproto.Update) error
	Toggle(ctx context.Context, userID int64, action installed.Action, sets []installed.Ref) error
	Reorder(ctx context.Context, userID int64, kind installed.Kind, order []int64) ([]int64, error)
}

type ServiceContext struct {
	Config    config.Config
	Catalog   Catalog
	Media     media_client.MediaClient
	Demand    Demand // nil without a bot
	Installed Installed

	// The reactions to messages (task 053).
	Msg       msg_client.MsgClient
	Message   message_client.MessageClient
	User      user_client.UserClient
	Chat      chat_client.ChatClient
	Reactions *sqlx.DB // message_reactions, saved_reaction_tags, user_reaction_settings: reads and the default reaction
}

func NewServiceContext(c config.Config) *ServiceContext {
	store := catalog.NewStore(sqlx.NewMySQL(&c.Mysql))
	media := media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient))
	if err := outbox.Register(outbox.Topics{Push: []string{c.SyncTopic}}); err != nil {
		logx.Must(err)
	}
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		logx.Must(err)
	}
	s := &ServiceContext{
		Config:    c,
		Catalog:   store,
		Media:     media,
		Installed: installed.NewStore(sqlx.NewMySQL(&c.Mysql), boxDB, c.SyncTopic),
		Msg:       msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		Message:   message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.MessageClient)),
		User:      user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		Chat:      chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		Reactions: sqlx.NewMySQL(&c.Mysql),
	}
	bot, err := tgbot.FromSettings(c.TelegramBot)
	if err != nil {
		logx.Must(err)
	}
	if bot != nil {
		s.Demand = demand.New(bot, &importer.Importer{
			Catalog: store,
			Documents: importer.RPCDocuments{
				Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
				Media: media,
			},
			Now: func() int64 { return time.Now().Unix() },
		})
	}
	return s
}
