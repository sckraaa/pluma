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
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/releases/internal/config"
	"github.com/teamgram/teamgram-server/pkg/catalog/importer"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/core/logx"
)

var configFile = flag.String("f", "etc/releases.yaml", "the config file")

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
	// The config carries secrets from the environment: never log it whole.
	logx.Infof("loaded config %s", *configFile)

	token, err := os.ReadFile(c.UploadTokenFile)
	if err != nil {
		return fmt.Errorf("releases: the upload token: %w", err)
	}
	h := &Handler{
		AdminToken: strings.TrimSpace(c.AdminToken),
		PublicURL:  strings.TrimRight(c.PublicURL, "/"),
		DataDir:    c.DataDir,
		Token:      strings.TrimSpace(string(token)),
		DB:         sqlx.NewMySQL(&c.Mysql),
		Documents: importer.RPCDocuments{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		},
		Now: time.Now,
	}
	if len(h.Token) < 32 {
		return errors.New("releases: the upload token is shorter than 32 characters")
	}
	// Uploads cut off by a stop left their parts in work/; none runs now.
	if err = os.RemoveAll(filepath.Join(c.DataDir, "work")); err != nil {
		return fmt.Errorf("releases: clear the work directory: %w", err)
	}
	for _, d := range []string{filepath.Join(c.DataDir, "files"), filepath.Join(c.DataDir, "work")} {
		if err = os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	s.http = &http.Server{
		Addr:              c.ListenOn,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Uploads of a few hundred MB from CI. Downloads have no deadline
		// of their own: each write moves it on (progressWriter), and other
		// answers are short.
		ReadTimeout:  30 * time.Minute,
		WriteTimeout: 0,
		IdleTimeout:  2 * time.Minute,
	}
	go func() {
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorf("releases: %v", err)
			os.Exit(1)
		}
	}()
	return nil
}

func (s *Server) RunLoop() {}

func (s *Server) Destroy() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
}
