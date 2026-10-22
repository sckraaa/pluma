/*
 * WARNING! All changes made in this file will be lost!
 *   Created from by 'dalgen'
 *
 * Copyright (c) 2024-present,  Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package mysql_dao

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/dal/dataobject"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserPtsUpdatesDAO struct {
	db *sqlx.DB
}

func NewUserPtsUpdatesDAO(db *sqlx.DB) *UserPtsUpdatesDAO {
	return &UserPtsUpdatesDAO{
		db: db,
	}
}

// SelectByPtsRange
// select user_id, pts, pts_count, update_type, update_data from user_pts_updates where user_id = :user_id and pts > :pts and pts <= :max_pts order by pts asc limit :limit
func (dao *UserPtsUpdatesDAO) SelectByPtsRange(ctx context.Context, userId int64, pts int32, maxPts int32, limit int32) (rList []dataobject.UserPtsUpdatesDO, err error) {
	var (
		query  string
		values []dataobject.UserPtsUpdatesDO
	)
	query = "select user_id, pts, pts_count, update_type, update_data from user_pts_updates where user_id = ? and pts > ? and pts <= ? order by pts asc limit ?"

	err = dao.db.QueryRowsPartial(ctx, &values, query, userId, pts, maxPts, limit)

	if err != nil {
		logx.WithContext(ctx).Errorf("queryx in SelectByPtsRange(_), error: %v", err)
		return
	}

	rList = values

	return
}
