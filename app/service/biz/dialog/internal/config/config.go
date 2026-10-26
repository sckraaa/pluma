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
	Mysql sqlx.Config
	Cache cache.CacheConf
	// Topics of the events the box transactions of the service write
	// (mq_outbox, ADR 0012): the pushes of updates with seq (ADR 0020).
	Topics Topics
}

// Topics are the Kafka topics of the service's events.
type Topics struct {
	Sync string // pushes to devices (Sync-T)
}
