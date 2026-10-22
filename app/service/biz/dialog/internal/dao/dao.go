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

	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/config"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

type Dao struct {
	*Mysql
	sqlc.CachedConn
	// BoxDB is the pool of box transactions (pkg/userbox): the *sql.DB of
	// Mysql. A change of several dialog rows of a user goes through RunBox,
	// so it locks the user's box first, as msg does (ADR 0011, p. 3).
	BoxDB *sql.DB
}

func New(c config.Config) (dao *Dao) {
	db := sqlx.NewMySQL(&c.Mysql)
	boxDB, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(err)
	}
	return &Dao{
		Mysql:      newMysqlDao(db),
		CachedConn: sqlc.NewConn(db, c.Cache),
		BoxDB:      boxDB,
	}
}

// RunBox changes userId's dialog rows in one box transaction (userbox.Run).
// rowKeys are deleted from the cache before the transaction, and again with
// the keys fn added after commit. A failed deletion is logged.
func (d *Dao) RunBox(ctx context.Context, userId int64, rowKeys []string, fn func(b *userbox.Box) error) error {
	d.deleteCache(ctx, rowKeys)
	res, err := userbox.Run(ctx, d.BoxDB, userId, fn)
	if err != nil {
		return err
	}
	d.deleteCache(ctx, append(rowKeys, res.CacheKeys...))
	return nil
}

func (d *Dao) deleteCache(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dialog.CacheTimeout)
	defer cancel()
	if err := d.CachedConn.DelCache(cctx, keys...); err != nil {
		logx.WithContext(ctx).Errorf("dialog: delete %d cache keys: %v", len(keys), err)
	}
}
