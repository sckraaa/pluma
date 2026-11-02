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

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/importer"
	"github.com/teamgram/teamgram-server/pkg/catalog/receiver"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/zeromicro/go-zero/zrpc"
)

// openCatalogImporter returns the importer over the services of the
// config; replaced by the tests.
var openCatalogImporter = func(configFile string) (catalogImporter, error) {
	var c struct {
		Mysql       sqlx.Config
		DfsClient   zrpc.RpcClientConf
		MediaClient zrpc.RpcClientConf
	}
	if err := conf.Load(configFile, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", configFile, err)
	}
	return &importer.Importer{
		Catalog: catalog.NewStore(sqlx.NewMySQL(&c.Mysql)),
		Documents: importer.RPCDocuments{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		},
		Now: func() int64 { return time.Now().Unix() },
	}, nil
}

type catalogImporter interface {
	Run(ctx context.Context, dir string) (*importer.Result, error)
}

// catalogCommand runs catalog receive and import (task 024, ADR 0028;
// docs/ops/catalog.md): receive takes a collection from the collector in a
// tab of Telegram Web K, import puts it in place of the catalog the server
// answers with.
func catalogCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) >= 2 && args[1] == "receive" {
		return catalogReceive(args[2:], stdout)
	}
	if len(args) != 3 || args[1] != "import" {
		return errUsage
	}
	im, err := openCatalogImporter(configFile)
	if err != nil {
		return err
	}
	if i, ok := im.(*importer.Importer); ok {
		i.Logf = func(format string, a ...any) { fmt.Fprintf(stdout, format+"\n", a...) }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	r, err := im.Run(ctx, args[2])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "catalog: %d answers, %d sets, %d documents (%d stored now); %d references to sets Telegram does not give out left empty.\n",
		r.Answers, r.Sets, r.Documents, r.NewDocuments, r.UnsharedSets)
	return nil
}

// catalogReceive serves the receiver until stopped; the token of the run is
// $CATALOG_UPLOAD_TOKEN (at least receiver.MinToken characters).
func catalogReceive(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("catalog receive", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "the directory of the collection")
	listen := flags.String("listen", ":9099", "the address to listen on")
	if err := flags.Parse(args); err != nil || *dir == "" || flags.NArg() != 0 {
		return errUsage
	}
	if err := os.MkdirAll(*dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(*dir)
	if err != nil {
		return err
	}
	defer root.Close()
	logf := func(format string, a ...any) { fmt.Fprintf(stdout, format+"\n", a...) }
	r, err := receiver.New(root, os.Getenv("CATALOG_UPLOAD_TOKEN"), logf)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	logf("receiver: listening on %s", *listen)
	return srv.ListenAndServe()
}
