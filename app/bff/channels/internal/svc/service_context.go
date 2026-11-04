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
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/config"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
)

type ServiceContext struct {
	Config  config.Config
	Channel channel_client.ChannelClient
	User    user_client.UserClient
	Chat    chat_client.ChatClient
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:  c,
		Channel: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		User:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		Chat:    chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
	}
}
