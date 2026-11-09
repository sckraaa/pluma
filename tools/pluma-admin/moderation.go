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
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"

	"github.com/zeromicro/go-zero/zrpc"
)

// moderationConfig is the part of the bff config moderation needs: the
// emergency access to the moderation of the panel, by SSH (ADR 0040).
type moderationConfig struct {
	Mysql             sqlx.Config
	BizServiceClient  zrpc.RpcClientConf
	MsgClient         zrpc.RpcClientConf
	AuthSessionClient zrpc.RpcClientConf
	SyncClient        struct{ Topic string }
}

// moderationCommand runs restrict, ban, lift and reports.
func moderationCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	var (
		phone string
		days  int
		all   bool
	)
	switch args[1] {
	case "restrict", "ban":
		if len(args) < 3 {
			return errUsage
		}
		flags := flag.NewFlagSet("moderation", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.IntVar(&days, "days", 0, "days of the restriction; without it, for good")
		if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 || days < 0 {
			return errUsage
		}
		var err error
		if phone, err = normalize(args[2]); err != nil {
			return err
		}
	case "lift":
		if len(args) != 3 {
			return errUsage
		}
		var err error
		if phone, err = normalize(args[2]); err != nil {
			return err
		}
	case "reports":
		flags := flag.NewFlagSet("moderation", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.BoolVar(&all, "all", false, "the decided ones too")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
			return errUsage
		}
	default:
		return errUsage
	}

	var c moderationConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	dao := moderation.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if args[1] == "reports" {
		return listReports(ctx, dao, all, stdout)
	}
	var rows []struct {
		Id int64 `db:"id"`
	}
	if err := db.QueryRowsPartial(ctx, &rows, "SELECT id FROM users WHERE phone = ? AND deleted = 0", phone); err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("+%s has no account", phone)
	}
	userId := rows[0].Id
	if args[1] == "lift" {
		lifted, err := dao.Lift(ctx, userId, "pluma-admin", "pluma-admin")
		if err != nil {
			return err
		}
		if !lifted {
			fmt.Fprintf(stdout, "+%s has no restriction.\n", phone)
			return nil
		}
		fmt.Fprintf(stdout, "+%s (user %d) is free.\n", phone, userId)
		return nil
	}
	var until int32
	if days > 0 {
		until = int32(time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix())
	}
	runner := &effects.Runner{
		DAO:         dao,
		Msg:         msg_client.NewMsgClient(rpcx.GetCachedRpcClient(c.MsgClient)),
		Channel:     channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
		Authsession: authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthSessionClient)),
		Sync:        sync_client.MustNewSyncOutboxClient(sync_client.OutboxConf{DSN: c.Mysql.DSN, Topic: c.SyncClient.Topic}),
	}
	if err := runner.ApplyDirect(ctx, userId, args[1] == "ban", until, "pluma-admin", "pluma-admin"); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "+%s (user %d): %s.\n", phone, userId, args[1])
	return nil
}

func listReports(ctx context.Context, dao *moderation.DAO, all bool, stdout io.Writer) error {
	status := moderation.StatusOpen
	if all {
		status = moderation.StatusDecided
	}
	rows, err := dao.Queue(ctx, status, 0, 100)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tREPORTER\tKIND\tPEER\tTARGET\tREASON\tDECISION")
	for _, r := range rows {
		fmt.Fprintf(w, "%d\t%d\t%d\t%d/%d\t%d\t%s\t%s\n", r.Id, r.ReporterUserId, r.Kind, r.PeerType, r.PeerId, r.TargetUserId, r.Reason, r.Decision)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d report(s).\n", len(rows))
	return nil
}

// panelCommand manages the first owner and the passkeys of the panel from
// the server, for what the panel itself cannot do without one.
func panelCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) != 3 {
		return errUsage
	}
	var c moderationConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	login := args[2]
	switch args[1] {
	case "owner":
		if _, err := db.Exec(ctx,
			"INSERT INTO admin_users (login, role, created_by, created_at) VALUES (?, 2, 'pluma-admin', ?) ON DUPLICATE KEY UPDATE role = 2, disabled = 0",
			login, time.Now().Unix()); err != nil {
			return err
		}
		if err := moderation.New(db).Audit(ctx, "pluma-admin", "admin_owner", "admin:"+login, ""); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s is an owner of the panel.\n", login)
	case "reset-passkey":
		if _, err := db.Exec(ctx, "DELETE FROM admin_passkeys WHERE login = ?", login); err != nil {
			return err
		}
		if _, err := db.Exec(ctx, "DELETE FROM admin_sessions WHERE login = ?", login); err != nil {
			return err
		}
		if err := moderation.New(db).Audit(ctx, "pluma-admin", "passkey_reset", "admin:"+login, ""); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s has no passkey: the panel will ask for a new one.\n", login)
	default:
		return errUsage
	}
	return nil
}
