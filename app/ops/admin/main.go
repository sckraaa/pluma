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

// Command admin is the internal moderation panel (task 025,
// ADR 0040). It listens on a loopback address only and is published to the
// tailnet by tailscale serve, whose identity headers it trusts on loopback
// requests, with a passkey on top (docs/ops/admin-panel.md).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"
	"github.com/teamgram/teamgram-server/pkg/twofa"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

var configFile = flag.String("f", "etc/admin.yaml", "the config file")

// The topics of the events the System page shows, as topics.sh creates
// them; the sync topic is SyncClient.Topic.
const (
	inboxTopic   = "Inbox-T"
	botSyncTopic = "BotSync"
)

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
	st := &store{db: db}
	biz := rpcx.GetCachedRpcClient(c.BizServiceClient)
	msg := msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient))
	auth := authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthSessionClient))
	channels := channel_client.NewChannelClient(biz)
	runner := &effects.Runner{
		DAO:         dao,
		Msg:         msg,
		Channel:     channels,
		Authsession: auth,
		Sync:        sync_client.MustNewSyncOutboxClient(sync_client.OutboxConf{DSN: c.Mysql.DSN, Topic: c.SyncClient.Topic}),
	}
	boxDB, err := openOutboxDB(&c)
	if err != nil {
		log.Fatalf("admin: outbox: %v", err)
	}
	codes := logincode.MustNewService(c.LoginCode, c.Mysql, c.KV)
	d := deps{
		dao:       dao,
		runner:    runner,
		dfs:       dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
		channel:   channels,
		chat:      chat_client.NewChatClient(biz),
		user:      user_client.NewUserClient(biz),
		auth:      auth,
		msg:       msg,
		codes:     codes,
		twofa:     twofa.MustNewService(c.Mysql, c.KV),
		allowlist: phoneallowlist.New(db),
		outbox:    &outboxAdmin{db: boxDB},
		prom:      newPromClient(c.Prometheus),
		releases:  newReleasesClient(c.Releases.Url, c.Releases.Token),
	}
	srv := &http.Server{
		Addr:              c.Listen,
		Handler:           newServer(&c, st, d).routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go runWorker(ctx, dao, runner, st)
	go runBroadcasts(ctx, st, msg)
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

// openOutboxDB opens mq_outbox for the operator's actions of the System
// page, with the session settings of the box transactions (pkg/userbox) and
// the topics of the deployment registered, as pluma-admin outbox does.
func openOutboxDB(c *config) (*sql.DB, error) {
	if err := outbox.Register(outbox.Topics{Inbox: []string{inboxTopic}, Push: []string{c.SyncClient.Topic, botSyncTopic}}); err != nil {
		return nil, err
	}
	dsn, err := mysql.ParseDSN(c.Mysql.DSN)
	if err != nil {
		return nil, err
	}
	if dsn.Params == nil {
		dsn.Params = map[string]string{}
	}
	dsn.Params["innodb_lock_wait_timeout"] = strconv.Itoa(int(userbox.LockWaitTimeout.Seconds()))
	return userbox.Open(dsn.FormatDSN())
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
