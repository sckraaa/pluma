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

// Command admin is the internal moderation and Premium panel (task 025,
// ADR 0039). It listens on a loopback address only and is published to the
// tailnet by tailscale serve, whose identity headers it trusts on loopback
// requests, with a passkey on top (docs/ops/admin-panel.md).
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"

	"github.com/zeromicro/go-zero/core/logx"
)

var configFile = flag.String("f", "etc/admin.yaml", "the config file")

// workerPeriod is how often the steps of decisions left over after a
// failure or a restart are tried again.
const workerPeriod = 30 * time.Second

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	flag.Parse()
	var c config
	if err := conf.Load(*configFile, &c); err != nil {
		log.Fatalf("admin: config: %v", err)
	}
	if err := c.check(); err != nil {
		log.Fatalf("admin: %v", err)
	}
	logx.Must(logx.SetUp(c.Log))
	defer logx.Close()

	db := sqlx.NewMySQL(&c.Mysql)
	dao := moderation.New(db)
	msg := msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient))
	runner := &effects.Runner{
		DAO:         dao,
		Msg:         msg,
		Channel:     channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
		Authsession: authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthSessionClient)),
		Sync:        sync_client.MustNewSyncOutboxClient(sync_client.OutboxConf{DSN: c.Mysql.DSN, Topic: c.SyncClient.Topic}),
	}
	srv := &http.Server{
		Addr:              c.Listen,
		Handler:           newServer(&c, &store{db: db}, dao, runner, user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)), dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient))).routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go runWorker(ctx, dao, runner, &store{db: db})
	go func() {
		<-ctx.Done()
		sctx, c2 := context.WithTimeout(context.Background(), 20*time.Second)
		defer c2()
		if err := srv.Shutdown(sctx); err != nil {
			logx.Errorf("admin: shutdown: %v", err)
		}
	}()
	logx.Infof("admin: listening on %s", c.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logx.Close()
		log.Fatalf("admin: %v", err)
	}
}

// runWorker carries on the decisions whose steps were interrupted and
// drops what expired.
func runWorker(ctx context.Context, dao *moderation.DAO, runner *effects.Runner, st *store) {
	tick := time.NewTicker(workerPeriod)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := st.prune(ctx); err != nil {
			logx.Errorf("admin: prune: %v", err)
		}
		ids, err := dao.PendingEffects(ctx, 20)
		if err != nil {
			logx.Errorf("admin: pending effects: %v", err)
			continue
		}
		for _, id := range ids {
			rctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			if err := runner.Run(rctx, id); err != nil {
				logx.Errorf("admin: effects of report %d: %v", id, err)
			}
			cancel()
		}
	}
}

// healthcheck is the container's health check: the image has no shell.
func healthcheck() int {
	flag.Parse()
	var c config
	if err := conf.Load(*configFile, &c); err != nil {
		return 1
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + c.Listen + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
