/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package user_helper

import (
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/core"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/svc"
)

type (
	Dao           = dao.Dao
	CacheUserData = dao.CacheUserData
)

var (
	GenCacheUserDataCacheKey = dao.GenCacheUserDataCacheKey
)

type (
	Config       = config.Config
	Topics       = config.Topics
	Service      = service.Service
	SignUpWorker = core.SignUpWorker
	Workers      = core.Workers
)

var (
	NewServiceContext = svc.NewServiceContext
)

const (
	// MaxBoxOpTime is the longest a box operation of the service takes:
	// the deadline of the server must cover it.
	MaxBoxOpTime = dao.MaxBoxOpTime
	// SignUpStopWait is how long stopping the sign-up worker may take.
	SignUpStopWait = core.SignUpStopWait
	// PremiumStopWait is how long stopping the Premium expiry may take.
	PremiumStopWait = core.PremiumStopWait
)

// New returns the user service and its workers: the delivery of the
// sign-ups of imported numbers to their importers and the expiry of
// Premium grants. The server starts the workers and stops them when it
// stops.
func New(c Config) (*service.Service, *core.Workers) {
	ctx := svc.NewServiceContext(c)
	return service.New(ctx), &core.Workers{
		SignUps: core.NewSignUpWorker(ctx),
		Premium: core.NewPremiumWorker(ctx),
	}
}
