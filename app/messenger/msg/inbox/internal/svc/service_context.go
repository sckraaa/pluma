/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package svc

import (
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
)

type ServiceContext struct {
	*dao.Dao
}

func NewServiceContext(d *dao.Dao) *ServiceContext {
	return &ServiceContext{Dao: d}
}
