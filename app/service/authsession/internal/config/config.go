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
	"github.com/zeromicro/go-zero/core/stores/kv"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql sqlx.Config
	Cache cache.CacheConf
	KV    kv.KvConf
	// AuthKeyEncryptionKey seals the auth keys at rest: the hex of 32 random
	// bytes, ${AUTH_KEY_ENCRYPTION_KEY} (task 008, pkg/authkeys).
	AuthKeyEncryptionKey string
	// GeoIPDatabase is the path of the GeoIP database, an MMDB City database
	// such as MaxMind GeoLite City, that gives the place of a session.
	// authsession takes up a new file put there by a rename within 10
	// minutes; a file that cannot be used leaves the database read before
	// in use. Empty, or no usable file yet, leaves every place "UNKNOWN",
	// and authsession says so in its log (docs/ops/deployment.md, "GeoIP").
	GeoIPDatabase string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
