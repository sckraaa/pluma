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
	"context"
	"errors"
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/env2"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/app/interface/links/internal/config"
	"github.com/teamgram/teamgram-server/app/interface/links/internal/data"
	"github.com/teamgram/teamgram-server/app/interface/links/internal/links"
)

var configFile = flag.String("f", "etc/links.yaml", "the config file")

type Server struct {
	http *http.Server
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logx.Infof("loaded config %s", *configFile)

	biz := rpcx.GetCachedRpcClient(c.BizServiceClient)
	channels := channel_client.NewChannelClient(biz)
	source := &data.Source{
		User:    user_client.NewUserClient(biz),
		Channel: channels,
		Dfs:     dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
		Sets:    catalog.NewStore(sqlx.NewMySQL(&c.Mysql)),
	}
	b := &links.Builder{
		Site:   strings.TrimRight(c.SiteURL, "/"),
		Web:    strings.TrimRight(c.WebURL, "/"),
		Scheme: "plm",
		Data:   source,
	}
	if b.Site != "https://"+env2.TDotMe {
		return errors.New("links: SiteURL is not the link host https://" + env2.TDotMe)
	}
	h, err := links.NewHandler(b, source, &links.Previews{Builder: b, Data: &data.Preview{Source: source, Channels: channels}})
	if err != nil {
		return err
	}
	s.http = &http.Server{
		Addr:              c.ListenOn,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return nil
}

func (s *Server) RunLoop() {
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logx.Errorf("links: %v", err)
	}
}

func (s *Server) Destroy() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
}
