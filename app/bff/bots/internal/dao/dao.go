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

package dao

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/config"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Dao holds the clients the methods use, the store of bots and Redis.
type Dao struct {
	user_client.UserClient
	message_client.MessageClient
	dialog_client.DialogClient
	media_client.MediaClient
	msg_client.MsgClient
	sync_client.SyncClient
	Chat    chat_client.ChatClient
	Channel channel_client.ChannelClient

	Bots      *botstore.Store
	KV        kv.Store
	db        *sqlx.DB
	boxDB     *sql.DB
	syncTopic string
}

// New connects the clients.
func New(c config.Config) *Dao {
	if err := outbox.Register(outbox.Topics{Push: []string{c.SyncClient.Topic}}); err != nil {
		logx.Must(err)
	}
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		logx.Must(err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	return &Dao{
		UserClient:    user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		MessageClient: message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.MessageClient)),
		DialogClient:  dialog_client.NewDialogClient(rpcx.GetCachedRpcClient(c.DialogClient)),
		MediaClient:   media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		MsgClient:     msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		SyncClient:    sync_client.MustNewSyncOutboxClient(c.SyncClient),
		Chat:          chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		Channel:       channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		Bots:          botstore.New(db),
		KV:            kv.NewStore(c.KV),
		db:            db,
		boxDB:         boxDB,
		syncTopic:     c.SyncClient.Topic,
	}
}

// PushToBot writes u, an update with seq, to the journal of the bot and
// pushes it to the bot's sessions. An update of a press or a query missed
// while the bot was away comes with its updates.getDifference (other_updates
// of updates.difference).
func (d *Dao) PushToBot(ctx context.Context, botId int64, u *mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat) error {
	_, err := userbox.Run(ctx, d.boxDB, botId, func(b *userbox.Box) error {
		return sync_client.EmitSeqUpdate(b, d.syncTopic, u, users, chats)
	})
	if err != nil {
		return fmt.Errorf("bots: update to bot %d: %w", botId, err)
	}
	return nil
}

// ChannelPostAuthor returns who sent a message of a channel: a broadcast post
// shows no author (from_id), but keeps it in sender_user_id.
func (d *Dao) ChannelPostAuthor(ctx context.Context, channelId int64, msgId int32) (int64, error) {
	var rows []int64
	if err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, msgId); err != nil || len(rows) == 0 {
		return 0, err
	}
	return rows[0], nil
}
