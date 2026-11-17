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
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/env2"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"

	"github.com/teamgram/teamgram-server/app/interface/links/internal/config"
	"github.com/teamgram/teamgram-server/app/interface/links/internal/data"
	"github.com/teamgram/teamgram-server/app/interface/links/internal/links"
	"github.com/teamgram/teamgram-server/app/interface/links/internal/oauth"
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
	db := sqlx.NewMySQL(&c.Mysql)
	source := &data.Source{
		User:      user_client.NewUserClient(biz),
		Channel:   channels,
		Dfs:       dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
		Sets:      catalog.NewStore(db),
		ChatLinks: business.NewStore(db),
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
	if err = setupOAuth(c, h, b, db, source); err != nil {
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
	// A listener that fails ends the process, so that Docker restarts it,
	// as releases does.
	go func() {
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorf("links: %v", err)
			os.Exit(1)
		}
	}()
	return nil
}

// setupOAuth starts the provider of oauth.pluma.chat (task 106) on h when
// its signing key file holds a key; an empty file leaves it off, and its
// host answers as the site.
func setupOAuth(c config.Config, h *links.Handler, b *links.Builder, db *sqlx.DB, source *data.Source) error {
	keys, err := oauth.LoadKeys(c.OAuth.SigningKeyFile)
	if err != nil {
		return err
	}
	if keys == nil {
		logx.Infof("links: no OAuth signing key, the login on sites is off")
		return nil
	}
	issuer := strings.TrimRight(c.OAuth.Issuer, "/")
	if u, err := url.Parse(issuer); err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return errors.New("links: OAuth.Issuer is not an https:// origin")
	}
	sealer, err := c.BotSecrets.Sealer()
	if err != nil {
		return err
	}
	wordmark, err := links.Wordmark()
	if err != nil {
		return err
	}
	p := &oauth.Provider{
		Issuer:   issuer,
		Site:     b.Site,
		Keys:     keys,
		KV:       kv.NewStore(c.KV),
		Bots:     botstore.New(db),
		BotKeys:  botkeys.NewStore(db, sealer),
		Accounts: source,
		Regions:  oauth.AuthsessionRegions{Client: authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthsessionClient))},
		Texts:    links.SiteTexts{},
		Wordmark: wordmark,
	}
	if err = p.Init(); err != nil {
		return err
	}
	h.OAuth, h.OAuthHost = p, p.Host()
	return nil
}

func (s *Server) RunLoop() {}

func (s *Server) Destroy() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
}
