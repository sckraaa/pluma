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
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/config"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/channelsets"
	"github.com/teamgram/teamgram-server/pkg/sendas"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

type ServiceContext struct {
	Bots    *botstore.Store // bot_info of the full channel (task 070)
	Config  config.Config
	Channel channel_client.ChannelClient
	User    user_client.UserClient
	Chat    chat_client.ChatClient
	Twofa   *twofa.Service
	Media   media_client.MediaClient
	Auth    authsession_client.AuthsessionClient
	Msg     msg_client.MsgClient
	// Look reads the wallpaper and the chat theme of a channel (task 065).
	Look *appearance.Store
	// Sets are the sticker and emoji sets of supergroups (task 078).
	Sets *channelsets.Store
	// SendAs keeps the default peer of channelFull.default_send_as
	// (task 079).
	SendAs *sendas.Store
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:  c,
		Channel: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		User:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		Chat:    chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		Twofa:   twofa.MustNewService(c.Mysql, c.KV),
		Media:   media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		Auth:    authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthsessionClient)),
		Msg:     msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		Look:    appearance.NewStore(sqlx.NewMySQL(&c.Mysql), nil, ""),
		Bots:    botstore.New(sqlx.NewMySQL(&c.Mysql)),
		Sets:    channelsets.New(sqlx.NewMySQL(&c.Mysql)),
		SendAs:  sendas.New(sqlx.NewMySQL(&c.Mysql)),
	}
}
