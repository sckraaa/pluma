/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2024-present, Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package server

import (
	"flag"

	"github.com/teamgram/teamgram-server/app/interface/httpserver/internal/config"
	"github.com/teamgram/teamgram-server/app/interface/httpserver/internal/server/http"
	"github.com/teamgram/teamgram-server/app/interface/httpserver/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/httpserver.yaml", "the config file")

type Server struct {
	httpSrv *rest.Server
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets from the environment (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)
	ctx := svc.NewServiceContext(c)

	s.httpSrv = http.New(ctx, c.Http)

	return nil
}

func (s *Server) RunLoop() {
}

func (s *Server) Destroy() {
}
