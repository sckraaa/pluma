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

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/server/grpc"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

var configFile = flag.String("f", "etc/chat.yaml", "the config file")

type Server struct {
	grpcSrv *zrpc.RpcServer
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)
	// Alone, the service asks biz user for the users of the operator's
	// own updates with seq.
	ctx := svc.NewServiceContext(c, nil, user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)))
	s.grpcSrv = grpc.New(ctx, c.RpcServerConf)

	go func() {
		go s.grpcSrv.Start()
	}()
	return nil
}

func (s *Server) RunLoop() {
}

func (s *Server) Destroy() {
	s.grpcSrv.Stop()
}
