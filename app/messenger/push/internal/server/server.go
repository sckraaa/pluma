// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"flag"

	"github.com/teamgram/teamgram-server/app/messenger/push/internal/config"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/server/mq"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"

	"github.com/zeromicro/go-zero/core/logx"
)

var configFile = flag.String("f", "etc/push.yaml", "the config file")

type Server struct {
	mq *mqconsumer.Consumer
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)
	// The config carries secrets from the environment: never log it whole.
	logx.Infof("loaded config %s", *configFile)

	// push runs no gRPC server, which would set these up: logs, metrics
	// endpoint (Prometheus, health check of the compose service), tracing.
	if err := c.SetUp(); err != nil {
		return err
	}
	ctx := svc.NewServiceContext(c)
	s.mq = mq.New(c.PushConsumer, ctx.Core)
	s.mq.Start()
	return nil
}

func (s *Server) RunLoop() {
}

func (s *Server) Destroy() {
	s.mq.Stop()
}
