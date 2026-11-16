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
	"fmt"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"

	"golang.org/x/sync/singleflight"
)

// Channels reads a channel with the participant rows of some users.
type Channels interface {
	ChannelGetMutableChannel(ctx context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error)
}

// ChannelMessages reads messages of channels and the channels themselves as
// a viewer sees them: the copies the public forwards of a post or a story
// list, and the channels of those copies.
type ChannelMessages interface {
	ChannelGetMessages(ctx context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error)
	ChannelGetChannelListByIdList(ctx context.Context, in *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error)
}

// Users reads the users the statistics name (the top members of a
// supergroup) as the viewer sees them.
type Users interface {
	UserGetMutableUsersV2(ctx context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error)
}

type ServiceContext struct {
	Config   config.Config
	Dao      *dao.Dao
	Channels Channels
	Messages ChannelMessages
	Users    Users
	// Signer signs and checks the zoom tokens; nil when the statistics are
	// off (no STATS_GRAPH_TOKEN_KEY).
	Signer *statsgraph.Signer
	// Flight joins the same request of a viewer made again while the first
	// one is built (a client that opens the screen twice, iOS retrying).
	Flight *singleflight.Group
}

func NewServiceContext(c config.Config) *ServiceContext {
	if err := c.Stats.Validate(); err != nil {
		panic(err)
	}
	channels := channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient))
	s := &ServiceContext{
		Config:   c,
		Dao:      dao.New(sqlx.NewMySQL(&c.Mysql)),
		Channels: channels,
		Messages: channels,
		Users:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		Flight:   new(singleflight.Group),
	}
	if c.Stats.Enabled() {
		key, err := statsgraph.ParseKey(c.Stats.TokenKey)
		if err != nil {
			panic(fmt.Errorf("stats: %w", err))
		}
		if s.Signer, err = statsgraph.NewSigner(key); err != nil {
			panic(fmt.Errorf("stats: %w", err))
		}
	}
	return s
}
