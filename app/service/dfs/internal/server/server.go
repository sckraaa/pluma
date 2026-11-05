/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package server

import (
	"context"
	"flag"
	"time"

	"github.com/zeromicro/go-zero/rest"

	"github.com/teamgram/teamgram-server/app/service/dfs/internal/server/http"

	"github.com/teamgram/teamgram-server/app/service/dfs/internal/config"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/server/grpc"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

var configFile = flag.String("f", "etc/dfs.yaml", "the config file")

type Server struct {
	grpcSrv *zrpc.RpcServer
	httpSrv *rest.Server
	svcCtx  *svc.ServiceContext
	stop    context.CancelFunc
}

// persistGrace is how long a stop waits for the background stores: within
// the stop_grace_period of dfs (30 s, docker-compose.yaml).
const persistGrace = 25 * time.Second

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets from the environment (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)
	ctx := svc.NewServiceContext(c)
	s.svcCtx = ctx
	sweepCtx, stop := context.WithCancel(context.Background())
	s.stop = stop
	go ctx.Dao.SweepParts(sweepCtx)

	s.grpcSrv = grpc.New(ctx, c.RpcServerConf)
	go func() {
		s.grpcSrv.Start()
	}()

	s.httpSrv = http.New(ctx, c.MiniHttp)

	return nil
}

func (s *Server) RunLoop() {
}

func (s *Server) Destroy() {
	s.stop()
	// First: Stop closes the log. Stores asked for meanwhile are recorded
	// and resume at the next start.
	ctx, cancel := context.WithTimeout(context.Background(), persistGrace)
	defer cancel()
	s.svcCtx.Dao.WaitPersisted(ctx)
	s.grpcSrv.Stop()
}
