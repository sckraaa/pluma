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
	// UserClient looks up the users of the operator's own updates with seq
	// when the service runs alone; the biz server gives it its user service
	// and leaves this empty.
	UserClient zrpc.RpcClientConf `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Topics of the events the box transactions of the service write
	// (ADR 0012, 0021): the updates with seq of basic groups to their
	// members.
	Topics Topics
}

// Topics are the Kafka topics of the service's events.
type Topics struct {
	Inbox string // inbox events, applied in the recipient's box (Inbox-T)
	Sync  string // pushes to devices (Sync-T)
}
