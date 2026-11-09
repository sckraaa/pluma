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
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/updates/internal/config"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	updates_client "github.com/teamgram/teamgram-server/app/service/biz/updates/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"
)

type Dao struct {
	updates_client.UpdatesClient
	user_client.UserClient
	chat_client.ChatClient
	channel_client.ChannelClient
	dialog_client.DialogClient
	authsession_client.AuthsessionClient
	// Calls is the table of one-to-one calls (task 068).
	Calls *phonecalls.Store
}

func New(c config.Config) *Dao {
	return &Dao{
		Calls:             phonecalls.NewStore(sqlx.NewMySQL(&c.Mysql)),
		UpdatesClient:     updates_client.NewUpdatesClient(rpcx.GetCachedRpcClient(c.UpdatesClient)),
		UserClient:        user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		ChatClient:        chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		ChannelClient:     channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		DialogClient:      dialog_client.NewDialogClient(rpcx.GetCachedRpcClient(c.DialogClient)),
		AuthsessionClient: authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthsessionClient)),
	}
}
