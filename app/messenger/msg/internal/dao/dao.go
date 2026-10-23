/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package dao

import (
	"context"
	"database/sql"
	"fmt"

	kafka "github.com/teamgram/marmota/pkg/mq"
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/config"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	idgen_client "github.com/teamgram/teamgram-server/app/service/idgen/client"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

// Dao is the storage of msg: the RPC handlers (msg) and the inbox consumer
// share one, with one MySQL pool.
//
// Every change of a user's box (messages, dialogs columns of the box, the
// journal) is a box transaction, RunBox (pkg/userbox, ADR 0011). Its side
// effects — pushes to the user's devices and inbox events for other users —
// are events emitted inside the transaction and published after commit.
type Dao struct {
	*Mysql
	sqlc.CachedConn
	IdGenerator
	user_client.UserClient
	chat_client.ChatClient
	*redis.Redis

	// BoxDB is the pool of the box transactions: the *sql.DB of Mysql.
	BoxDB *sql.DB
	// Publisher sends the events of committed transactions.
	Publisher Publisher
	// Topics of the events.
	Topics Topics

	// idempotent opens the idempotency window of a send; tests replace it.
	idempotent func(key string) idempotentStore
}

// IdGenerator issues snowflake ids (idgen): dialog_message_id of messages.
type IdGenerator interface {
	NextId(ctx context.Context) (int64, error)
	NextIds(ctx context.Context, num int) ([]int64, error)
}

// Topics are the Kafka topics msg publishes to.
type Topics struct {
	Inbox   string // inbox events, consumed by msg itself
	Sync    string // pushes to users' devices
	BotSync string // pushes to bots; empty when bots are not served
}

// Publisher sends one event (pkg/userbox.Event) to its topic with the
// recipient as the key.
type Publisher interface {
	Publish(ctx context.Context, ev userbox.Event) error
}

// New connects the storage and the clients of msg and checks the MySQL
// settings box transactions rely on (userbox.Check): a wrong setting stops
// the service.
func New(ctx context.Context, c config.Config) (*Dao, error) {
	if c.MessageSharding > 1 {
		// Box transactions and the migration of user_state know one
		// messages table only (ADR 0011, p. 10).
		return nil, fmt.Errorf("msg: MessageSharding %d is not supported, want 1", c.MessageSharding)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		return nil, err
	}
	if err = userbox.Check(ctx, boxDB); err != nil {
		return nil, err
	}

	producers := map[string]producer{}
	topics := Topics{
		Inbox: c.InboxClient.Topic,
		Sync:  c.SyncClient.Topic,
	}
	producers[topics.Inbox] = kafka.GetCachedMQClient(c.InboxClient)
	producers[topics.Sync] = kafka.GetCachedMQClient(c.SyncClient)
	if c.BotSyncClient != nil {
		topics.BotSync = c.BotSyncClient.Topic
		producers[topics.BotSync] = kafka.GetCachedMQClient(c.BotSyncClient)
	}

	d := &Dao{
		Mysql:       NewMysqlDao(db, c.MessageSharding),
		CachedConn:  sqlc.NewConn(db, c.Cache),
		IdGenerator: idgenClient(c),
		UserClient:  user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
		ChatClient:  chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
		Redis:       redis.MustNewRedis(c.Redis2),
		BoxDB:       boxDB,
		Publisher:   newKafkaPublisher(producers),
		Topics:      topics,
	}
	return d, nil
}

func idgenClient(c config.Config) *idgen_client.IDGenClient2 {
	cli := idgen_client.NewIDGenClient2(rpcx.GetCachedRpcClient(c.IdgenClient))
	return &cli
}

// Close flushes and closes the Kafka producers.
func (d *Dao) Close() error {
	if p, ok := d.Publisher.(*kafkaPublisher); ok {
		return p.Close()
	}
	return nil
}
