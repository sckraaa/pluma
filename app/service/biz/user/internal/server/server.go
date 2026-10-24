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
	"flag"

	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/core"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/server/grpc"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

var configFile = flag.String("f", "etc/user.yaml", "the config file")

type Server struct {
	grpcSrv *zrpc.RpcServer
	signUps *core.SignUpWorker
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)
	ctx := svc.NewServiceContext(c)
	s.signUps = core.NewSignUpWorker(ctx)
	s.grpcSrv = grpc.New(ctx, c.RpcServerConf)

	go func() {
		go s.grpcSrv.Start()
	}()
	s.signUps.Start()
	return nil
}

func (s *Server) RunLoop() {
}

func (s *Server) Destroy() {
	s.signUps.Stop()
	s.grpcSrv.Stop()
}
