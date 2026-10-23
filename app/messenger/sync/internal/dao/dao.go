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
	"sync"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/messenger/sync/internal/config"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	status_client "github.com/teamgram/teamgram-server/app/service/status/client"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Dao struct {
	*Mysql
	kv               kv.Store
	conf             *config.Config
	mu               sync.RWMutex
	sessionServers   map[string]SessionPusher
	useStreamSession bool
	status_client.StatusClient
	chat_client.ChatClient
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	d := &Dao{
		Mysql:            newMysqlDao(db),
		kv:               kv.NewStore(c.KV),
		conf:             &c,
		sessionServers:   make(map[string]SessionPusher),
		useStreamSession: c.UseStreamSession,
		StatusClient:     status_client.NewStatusClient(zrpc.MustNewClient(c.StatusClient)),
		ChatClient:       chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
	}
	go d.watch(c.SessionClient)
	return d
}
