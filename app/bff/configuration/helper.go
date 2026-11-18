/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2022 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package configuration_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/svc"
)

type (
	Config = config.Config
	// EdgeConfig is the Edge block of bff.yaml (task 109).
	EdgeConfig = config.EdgeConfig
	// ProbeConfig is the Probe block of bff.yaml (task 113).
	ProbeConfig = config.ProbeConfig
)

func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}
