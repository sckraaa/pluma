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
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/config"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	"github.com/teamgram/teamgram-server/pkg/contactnotes"
	"github.com/teamgram/teamgram-server/pkg/contacttokens"
)

type Dao struct {
	user_client.UserClient
	chat_client.ChatClient
	sync_client.SyncClient
	// Channel loads the public channels contacts.search finds (task 055).
	Channel channel_client.ChannelClient
	// Notes are the private notes on contacts, Tokens the temporary links
	// to a profile (task 084).
	Notes  Notes
	Tokens Tokens
}

// Notes keeps the private notes on contacts (pkg/contactnotes).
type Notes interface {
	Set(ctx context.Context, userID, contactID int64, note *mtproto.TextWithEntities, now int64) error
}

// Tokens keeps the temporary links to a profile (pkg/contacttokens).
type Tokens interface {
	Export(ctx context.Context, userID, now int64) (token string, expires int64, err error)
	Import(ctx context.Context, token string, now int64) (int64, error)
}

func New(c config.Config) *Dao {
	return &Dao{
		UserClient: user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		ChatClient: chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		SyncClient: sync_client.MustNewSyncOutboxClient(c.SyncClient),
		Channel:    channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		Notes:      contactnotes.NewStore(sqlx.NewMySQL(&c.Mysql)),
		Tokens:     contacttokens.NewStore(sqlx.NewMySQL(&c.Mysql)),
	}
}
