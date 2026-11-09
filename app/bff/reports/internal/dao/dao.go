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
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/reports/internal/config"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// Dao holds the clients of the services a report's snapshot is read from
// and the store of the reports.
type Dao struct {
	user_client.UserClient
	message_client.MessageClient
	channel_client.ChannelClient
	Reports *moderation.DAO
}

// New connects the clients.
func New(c config.Config) *Dao {
	return &Dao{
		UserClient:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		MessageClient: message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.MessageClient)),
		ChannelClient: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		Reports:       moderation.New(sqlx.NewMySQL(&c.Mysql)),
	}
}
