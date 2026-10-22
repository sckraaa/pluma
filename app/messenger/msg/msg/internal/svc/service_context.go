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
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
)

type ServiceContext struct {
	plugin.MsgPlugin
	*dao.Dao
}

func NewServiceContext(d *dao.Dao, plugin plugin.MsgPlugin) *ServiceContext {
	svcCtx := &ServiceContext{
		MsgPlugin: plugin,
		Dao:       d,
	}
	if plugin == nil {
		svcCtx.MsgPlugin = d
	}
	return svcCtx
}
