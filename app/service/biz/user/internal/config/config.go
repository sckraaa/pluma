/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql       sqlx.Config
	Cache       cache.CacheConf
	MediaClient zrpc.RpcClientConf
	// IdgenClient issues the ids of the messages a contact's sign-up
	// sends to the users who imported the number (task 011).
	IdgenClient zrpc.RpcClientConf
	// Topics of the events the box transactions of the service write
	// (ADR 0012): the sign-up messages and the pushes of the new contact.
	Topics Topics
}

// Topics are the Kafka topics of the service's events, as in msg.
type Topics struct {
	Inbox string // inbox events, applied in the recipient's box (Inbox-T)
	Sync  string // pushes to devices (Sync-T)
}
