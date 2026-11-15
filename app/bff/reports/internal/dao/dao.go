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

// Package dao is the storage and the clients of the reports methods.
package dao

import (
	"context"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/reports/internal/config"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/readmetrics"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// Metrics keeps the read metrics and the music listens (pkg/readmetrics).
type Metrics interface {
	AddMetrics(ctx context.Context, userId int64, peerType int32, peerId int64, ms []readmetrics.Metric) (int64, error)
	AddListen(ctx context.Context, userId, documentId int64, duration int32) error
}

// Dao holds the clients of the services a report's snapshot is read from
// and the store of the reports.
type Dao struct {
	user_client.UserClient
	message_client.MessageClient
	channel_client.ChannelClient
	Chat        chat_client.ChatClient
	Media       media_client.MediaClient
	Reports     *moderation.DAO
	SecretChats *secretchat.Store
	ReadMetrics Metrics
	// Access keeps for a minute that a user may see a chat whose metrics
	// the user reports.
	Access *readmetrics.AccessCache
}

// New connects the clients.
func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	return &Dao{
		UserClient:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		MessageClient: message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.MessageClient)),
		ChannelClient: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		Chat:          chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		Media:         media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		Reports:       moderation.New(db),
		SecretChats:   secretchat.MustOpen(c.Mysql.DSN),
		ReadMetrics:   readmetrics.New(db),
		Access:        readmetrics.NewAccessCache(),
	}
}
