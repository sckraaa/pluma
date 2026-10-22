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
	"database/sql"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/config"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

type Dao struct {
	*Mysql
	// BoxDB reads the committed state of users' boxes (pkg/userbox).
	BoxDB *sql.DB
}

func New(c config.Config) *Dao {
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(err)
	}
	return &Dao{
		Mysql: newMysqlDao(db),
		BoxDB: boxDB,
	}
}
