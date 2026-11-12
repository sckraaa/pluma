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
	"context"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/users/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/users/internal/plugin"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/contactnotes"
)

type Dao struct {
	user_client.UserClient
	chat_client.ChatClient
	dialog_client.DialogClient
	media_client.MediaClient
	msg_client.MsgClient
	plugin.StoryPlugin
	plugin.PersonalChannelPlugin
	plugin.WallpaperPlugin
	Bots *botstore.Store
	// Notes are the private notes of an account on its contacts
	// (userFull.note, task 084).
	Notes Notes
}

// Notes reads the private notes on contacts (pkg/contactnotes).
type Notes interface {
	Get(ctx context.Context, userID, contactID int64) (*mtproto.TextWithEntities, error)
}

func New(c config.Config, plugin1 plugin.StoryPlugin, plugin2 plugin.PersonalChannelPlugin, plugin3 plugin.WallpaperPlugin) *Dao {
	return &Dao{
		UserClient:            user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		ChatClient:            chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		DialogClient:          dialog_client.NewDialogClient(rpcx.GetCachedRpcClient(c.DialogClient)),
		MediaClient:           media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		MsgClient:             msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		StoryPlugin:           plugin1,
		PersonalChannelPlugin: plugin2,
		WallpaperPlugin:       plugin3,
		Bots:                  botstore.New(sqlx.NewMySQL(&c.Mysql)),
		Notes:                 contactnotes.NewStore(sqlx.NewMySQL(&c.Mysql)),
	}
}
