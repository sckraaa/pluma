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
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	AuthSession      zrpc.RpcClientConf
	StatusClient     zrpc.RpcClientConf
	GatewayClient    zrpc.RpcClientConf
	BFFProxyClients  conf.BFFProxyClients
	UseStreamGateway bool `json:",default=false"`
	// RpcLimit bounds the requests of one auth key (task 008).
	RpcLimit RpcLimitConf `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// RpcLimitConf is a token bucket of RPCs per auth key: PerSecond requests a
// second on average, Burst at once. Over it the client gets FLOOD_WAIT_X.
// The defaults leave room for the parallel file parts of the official
// clients.
type RpcLimitConf struct {
	PerSecond int `json:",default=100"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Burst     int `json:",default=500"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Routine routine.
type Routine struct {
	Size uint64
	Chan uint64
}
