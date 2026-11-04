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
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/messenger/sync/internal/config"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	status_client "github.com/teamgram/teamgram-server/app/service/status/client"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Dao struct {
	*Mysql
	kv     kv.Store
	conf   *config.Config
	pusher *Pusher
	status_client.StatusClient
	chat_client.ChatClient
	// Channel and User build each member's view of a channel's updates
	// (task 039): biz channel and biz user, in the biz service of
	// ChatClient.
	Channel channel_client.ChannelClient
	User    user_client.UserClient
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	sessionConf := c.SessionClient
	d := &Dao{
		Mysql:        newMysqlDao(db),
		kv:           kv.NewStore(c.KV),
		conf:         &c,
		pusher:       newPusher(func(addr string) (sessionNode, error) { return newRpcNode(sessionConf, addr) }),
		StatusClient: status_client.NewStatusClient(zrpc.MustNewClient(c.StatusClient)),
		ChatClient:   chat_client.NewChatClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		Channel:      channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChatClient)),
		User:         user_client.NewUserClient(rpcx.GetCachedRpcClient(c.ChatClient)),
	}
	d.pusher.Start()
	watchNodes(c.SessionClient, d.pusher)
	return d
}

// Close stops pushing to the session nodes.
func (d *Dao) Close() {
	d.pusher.Close()
}
