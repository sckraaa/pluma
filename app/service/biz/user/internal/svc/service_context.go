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
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
)

type ServiceContext struct {
	Config config.Config
	*dao.Dao
	// SignUps is told about every new account: the sign-up of an imported
	// number is delivered to its importers (core.SignUpWorker). Without
	// it the account's sign-up waits in contact_signups for a worker.
	SignUps Notifier
}

// Notifier is woken by an event.
type Notifier interface {
	Notify()
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config: c,
		Dao:    dao.New(c),
	}
}
