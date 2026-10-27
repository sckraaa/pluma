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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/plugin"
)

type ServiceContext struct {
	Config config.Config
	*dao.Dao
}

// NewServiceContext builds the service on c; users looks up the users of
// the operator's own updates with seq.
func NewServiceContext(c config.Config, plugin plugin.ChatPlugin, users dao.UserSource) *ServiceContext {
	return &ServiceContext{
		Config: c,
		Dao:    dao.New(c, plugin, users),
	}
}
