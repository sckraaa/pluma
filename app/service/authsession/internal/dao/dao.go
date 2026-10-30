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
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/authsession/internal/config"
	"github.com/teamgram/teamgram-server/pkg/authkeys"

	"github.com/oschwald/geoip2-golang"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

type Dao struct {
	*Mysql
	sqlc.CachedConn
	kv   kv.Store
	MMDB *geoip2.Reader
	// keys seals the auth keys at rest (task 008).
	keys *authkeys.KeyCipher
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	// Without the encryption key authsession can neither store nor read a
	// key: it does not start.
	keys, err := authkeys.NewKeyCipher(c.AuthKeyEncryptionKey)
	logx.Must(err)

	return &Dao{
		Mysql:      newMysqlDao(db),
		CachedConn: sqlc.NewConn(db, c.Cache),
		kv:         kv.NewStore(c.KV),
		MMDB:       openGeoIP(c.GeoIPDatabase),
		keys:       keys,
	}
}
