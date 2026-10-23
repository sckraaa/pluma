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

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/config"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	idgen_client "github.com/teamgram/teamgram-server/app/service/idgen/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Dao is the storage of msg: the RPC handlers (msg) and the inbox consumer
// share one, with one MySQL pool.
//
// Every change of a user's box (messages, dialogs columns of the box, the
// journal) is a box transaction, RunBox (pkg/userbox, ADR 0011). Its side
// effects — pushes to the user's devices and inbox events for other users —
// are events written by the transaction to mq_outbox and published by the
// relay (pkg/outbox, ADR 0012).
type Dao struct {
	*Mysql
	sqlc.CachedConn
	IdGenerator
	user_client.UserClient
	chat_client.ChatClient

	// BoxDB is the pool of the box transactions: the *sql.DB of Mysql.
	BoxDB *sql.DB
	// Topics of the events.
	Topics Topics
}

// IdGenerator issues snowflake ids (idgen): dialog_message_id of messages.
type IdGenerator interface {
	NextId(ctx context.Context) (int64, error)
	NextIds(ctx context.Context, num int) ([]int64, error)
}

// Topics are the Kafka topics of msg's events.
type Topics = config.Topics

// RegisterTopics declares the topics of msg's events and their classes
// (outbox.Register).
func RegisterTopics(t Topics) error {
	if t.Inbox == "" || t.Sync == "" {
		return fmt.Errorf("msg: Topics.Inbox and Topics.Sync are required")
	}
	topics := outbox.Topics{Inbox: []string{t.Inbox}, Push: []string{t.Sync}}
	if t.BotSync != "" {
		topics.Push = append(topics.Push, t.BotSync)
	}
	return outbox.Register(topics)
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
	if err := RegisterTopics(c.Topics); err != nil {
		return nil, err
	}
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		return nil, err
	}
	if err = userbox.Check(ctx, boxDB); err != nil {
		return nil, err
	}
	return &Dao{
		Mysql:       NewMysqlDao(db, c.MessageSharding),
		CachedConn:  sqlc.NewConn(db, c.Cache),
		IdGenerator: idgenClient(c),
		UserClient:  user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
		ChatClient:  chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
		BoxDB:       boxDB,
		Topics:      c.Topics,
	}, nil
}

func idgenClient(c config.Config) *idgen_client.IDGenClient2 {
	cli := idgen_client.NewIDGenClient2(rpcx.GetCachedRpcClient(c.IdgenClient))
	return &cli
}
